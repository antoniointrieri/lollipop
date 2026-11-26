package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRemoteStateAndAuth(t *testing.T) {
	r := newRemoteServer(func(string) {})
	r.setToken("secret")
	r.publish([]item{{Key: "k1", Label: "api", State: "waiting"}})
	srv := httptest.NewServer(r.handler())
	defer srv.Close()

	if resp, _ := http.Get(srv.URL + "/state"); resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing token should be 401, got %v", resp)
	}
	if resp, _ := http.Get(srv.URL + "/state?token=wrong"); resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token should be 401, got %v", resp)
	}
	resp, err := http.Get(srv.URL + "/state?token=secret")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("state: %v %v", err, resp)
	}
	var got struct {
		Items []item `json:"items"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if len(got.Items) != 1 || got.Items[0].Label != "api" || got.Items[0].State != "waiting" {
		t.Fatalf("unexpected items: %+v", got.Items)
	}
}

func TestRemoteFocus(t *testing.T) {
	focused := make(chan string, 1)
	r := newRemoteServer(func(key string) { focused <- key })
	r.setToken("secret")
	srv := httptest.NewServer(r.handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/focus?token=secret", "application/json", strings.NewReader(`{"key":"k1"}`))
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("focus: %v %v", err, resp)
	}
	select {
	case key := <-focused:
		if key != "k1" {
			t.Fatalf("focused %q", key)
		}
	case <-time.After(time.Second):
		t.Fatal("focus callback not called")
	}
}

func TestRemoteEventsStream(t *testing.T) {
	r := newRemoteServer(func(string) {})
	r.setToken("secret")
	r.publish([]item{{Key: "k1", Label: "web", State: "working"}})
	srv := httptest.NewServer(r.handler())
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/events?token=secret", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("events: %v %v", err, resp)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if line := sc.Text(); strings.HasPrefix(line, "data: ") {
			if !strings.Contains(line, `"web"`) {
				t.Fatalf("unexpected event: %s", line)
			}
			return
		}
	}
	t.Fatal("no data event received")
}
