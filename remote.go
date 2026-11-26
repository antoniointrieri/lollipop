package main

// Local HTTP endpoint for the Android companion app. It serves the current agent state (one-shot and as an
// SSE stream) and accepts a "focus this agent" command, which reuses the same logic as a click in the bar.
// Off by default; the phone authenticates with a token (query parameter), and the state never leaves the LAN.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

const remotePort = 47600

type remoteServer struct {
	focus func(key string) // called on POST /focus; same action as a click in the bar

	mu       sync.Mutex
	token    string
	latest   []byte               // last published {"items":[...]} JSON, sent to a client on connect
	subs     map[chan []byte]bool // one channel per open SSE connection
	listener net.Listener         // nil when stopped
}

func newRemoteServer(focus func(key string)) *remoteServer {
	return &remoteServer{focus: focus, subs: map[chan []byte]bool{}}
}

func newToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (r *remoteServer) setToken(tok string) {
	r.mu.Lock()
	r.token = tok
	r.mu.Unlock()
}

// publish stores the latest state and pushes it to every open SSE connection. Called each poll while on.
func (r *remoteServer) publish(items []item) {
	msg := struct {
		Items []item `json:"items"`
	}{items}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	r.mu.Lock()
	r.latest = data
	for ch := range r.subs {
		select {
		case ch <- data:
		default: // a slow client never blocks the poll loop; it catches up on the next change
		}
	}
	r.mu.Unlock()
}

// ensureStarted starts the listener if it is not running. Safe to call every poll.
func (r *remoteServer) ensureStarted() {
	r.mu.Lock()
	if r.listener != nil {
		r.mu.Unlock()
		return
	}
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", remotePort))
	if err != nil {
		r.mu.Unlock()
		log.Println("remote.listen:", err)
		return
	}
	r.listener = ln
	r.mu.Unlock()
	srv := &http.Server{Handler: r.handler()}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Println("remote.serve:", err)
		}
	}()
}

// ensureStopped closes the listener and drops every open connection. Safe to call every poll.
func (r *remoteServer) ensureStopped() {
	r.mu.Lock()
	ln := r.listener
	r.listener = nil
	for ch := range r.subs {
		close(ch)
		delete(r.subs, ch)
	}
	r.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
}

func (r *remoteServer) checkToken(req *http.Request) bool {
	r.mu.Lock()
	want := r.token
	r.mu.Unlock()
	got := req.URL.Query().Get("token")
	return want != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (r *remoteServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/state", func(w http.ResponseWriter, req *http.Request) {
		if !r.checkToken(req) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.mu.Lock()
		data := r.latest
		r.mu.Unlock()
		if data == nil {
			data = []byte(`{"items":[]}`)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, req *http.Request) {
		if !r.checkToken(req) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		ch := make(chan []byte, 1)
		r.mu.Lock()
		r.subs[ch] = true
		if r.latest != nil {
			select {
			case ch <- r.latest:
			default:
			}
		}
		r.mu.Unlock()
		defer func() {
			r.mu.Lock()
			if r.subs[ch] {
				delete(r.subs, ch)
				close(ch)
			}
			r.mu.Unlock()
		}()

		heartbeat := time.NewTicker(20 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case <-req.Context().Done():
				return
			case data, open := <-ch:
				if !open {
					return
				}
				if _, err := fmt.Fprintf(w, "event: state\ndata: %s\n\n", data); err != nil {
					return
				}
				flusher.Flush()
			case <-heartbeat.C:
				if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	})
	mux.HandleFunc("/focus", func(w http.ResponseWriter, req *http.Request) {
		if !r.checkToken(req) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if req.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Key string `json:"key"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Key == "" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		r.focus(body.Key)
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

// lanAddress returns a "192.168.x.y:port" the phone can reach, or "" if no private IPv4 is found.
func lanAddress() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		ip := ipnet.IP.To4()
		if ip != nil && ip.IsPrivate() {
			return fmt.Sprintf("%s:%d", ip.String(), remotePort)
		}
	}
	return ""
}
