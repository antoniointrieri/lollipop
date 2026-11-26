package main

// Local HTTP endpoint for the Android companion app. It serves the current agent state (one-shot and as an
// SSE stream) and accepts a "focus this agent" command, which reuses the same logic as a click in the bar.
// Off by default; the phone authenticates with a token (query parameter), and the state never leaves the LAN.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"rsc.io/qr"
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

// maxLANAddresses caps the addresses in the pairing QR code, which grows denser with each one.
const maxLANAddresses = 4

// lanAddresses lists the "192.168.x.y:port" the phone may reach, best guess first: the address of the interface
// that holds the default route, then the private IPv4 addresses of the other interfaces that are up. Virtual
// adapters (WSL, Hyper-V, VPN) have private addresses too, and a VPN can hold the default route: the pairing QR
// code carries them all and the app keeps the first that answers.
func lanAddresses() []string {
	var ips []net.IP
	add := func(ip net.IP) {
		ip = ip.To4()
		if ip == nil || !ip.IsPrivate() || len(ips) == maxLANAddresses {
			return
		}
		for _, x := range ips {
			if x.Equal(ip) {
				return
			}
		}
		ips = append(ips, ip)
	}
	// A UDP "connection" sends no packet: it only asks the OS which local address the default route uses.
	if c, err := net.Dial("udp4", "192.0.2.1:9"); err == nil {
		if a, ok := c.LocalAddr().(*net.UDPAddr); ok {
			add(a.IP)
		}
		_ = c.Close()
	}
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok {
				add(ipnet.IP)
			}
		}
	}
	out := make([]string, len(ips))
	for i, ip := range ips {
		out[i] = fmt.Sprintf("%s:%d", ip, remotePort)
	}
	return out
}

// pairingURI is the text of the QR code in Settings → Phone: the app reads the addresses (one addr parameter
// each, best guess first) and the token from it.
func pairingURI(addrs []string, token string) string {
	return "lollipop://pair?" + url.Values{"addr": addrs, "token": {token}}.Encode()
}

// pairingQR renders pairingURI as an SVG data URL for the settings window, or "" while a part is missing.
func pairingQR(addrs []string, token string) string {
	if len(addrs) == 0 || token == "" {
		return ""
	}
	code, err := qr.Encode(pairingURI(addrs, token), qr.M)
	if err != nil {
		return ""
	}
	const quiet = 4 // white border, in modules, that scanners need around the code
	n := code.Size + 2*quiet
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`+
		`<rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, n, n, n, n)
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(b.String()))
}
