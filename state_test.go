package main

import (
	"encoding/json"
	"testing"
)

func TestBuildSnapshot(t *testing.T) {
	var wt worktreePS
	var tl terminalList
	var hooks lastStatus
	must := func(s string, v any) {
		if err := json.Unmarshal([]byte(s), v); err != nil {
			t.Fatal(err)
		}
	}
	must(`{"worktrees":[
		{"worktreeId":"w1","repo":"zeta","isMainWorktree":true,"isActive":true,"agents":[{"paneKey":"t1:l1","state":"working"}]},
		{"worktreeId":"w2","repo":"alfa","displayName":"feat","isMainWorktree":false,"agents":[
			{"paneKey":"t2:l2","state":"done"},{"paneKey":"t9:l9","state":"done"}]}]}`, &wt)
	must(`{"terminals":[{"handle":"h1","tabId":"t1","leafId":"l1","title":"✳ Claude Code"},{"handle":"h2","tabId":"t2","leafId":"l2","title":"plain"}],
		"visualLayouts":[{"worktreeId":"w1","root":{"activeTabId":"t1","tabs":[{"tabId":"t1","activeLeafId":"l1"}]}}]}`, &tl)
	must(`{"entries":{"t1:l1":{"payload":{"workingMode":"monitoring"}}}}`, &hooks)

	s := buildSnapshot(wt, tl, hooks)
	if len(s.Agents) != 2 { // t9:l9 has no terminal: closed session
		t.Fatalf("agenti: %+v", s.Agents)
	}
	if a := s.Agents[0]; a.Label != "alfa/feat" || a.Handle != "h2" || a.State != "done" {
		t.Errorf("worktree secondario: %+v", a)
	}
	if a := s.Agents[1]; a.Label != "zeta" || a.State != "monitoring" || a.Title != "Claude Code" {
		t.Errorf("worktree principale: %+v", a)
	}
	if s.Focused != "t1:l1" {
		t.Errorf("focused = %q", s.Focused)
	}
}

func TestTrackerBlink(t *testing.T) {
	tr := newTracker()
	snap := func(state, focused string) snapshot {
		return snapshot{Agents: []agent{{Key: "k", State: state}}, Focused: focused}
	}
	front := false
	blink := func(s snapshot) bool {
		return tr.update(s, func(a agent) bool { return front && a.Key == s.Focused })[0].Blink
	}

	if blink(snap("done", "")) {
		t.Error("all'avvio non deve lampeggiare")
	}
	blink(snap("working", ""))
	if !blink(snap("done", "")) {
		t.Error("working -> done deve lampeggiare")
	}
	if !blink(snap("done", "k")) {
		t.Error("pannello attivo ma Orca non in primo piano: deve continuare")
	}
	front = true
	if blink(snap("done", "k")) {
		t.Error("Orca in primo piano sul pannello: deve smettere")
	}
	blink(snap("working", ""))
	if blink(snap("waiting", "k")) {
		t.Error("gia' guardato quando finisce: non deve lampeggiare")
	}
	front = false
	blink(snap("working", ""))
	blink(snap("blocked", ""))
	tr.seen("k")
	if blink(snap("blocked", "")) {
		t.Error("dopo il clic deve smettere")
	}
	blink(snap("done", ""))
	if blink(snap("working", "")) {
		t.Error("tornato working: niente lampeggio")
	}
}

func TestSummary(t *testing.T) {
	items := []item{{State: "working"}, {State: "working"}, {State: "done"}, {State: "blocked"}}
	if c, tip := summary(items, ""); c != "red" || tip != "lollipop — 1 waiting, 1 done, 2 working" {
		t.Errorf("%s %q", c, tip)
	}
	if c, _ := summary(items[:3], ""); c != "green" {
		t.Errorf("senza attese: %s", c)
	}
	if c, tip := summary(nil, ""); c != "gray" || tip != "lollipop — no active agents" {
		t.Errorf("%s %q", c, tip)
	}
	if c, _ := summary(items, "pipe chiusa"); c != "red" {
		t.Errorf("errore: %s", c)
	}
}

func TestArrange(t *testing.T) {
	// input in alphabetical order, as the tracker returns it; seq: poll of the last state change
	in := func() []item {
		return []item{
			{Key: "a", State: "done", seq: 3},
			{Key: "b", State: "waiting", seq: 1},
			{Key: "c", State: "done", Blink: true, seq: 2},
			{Key: "d", State: "working", seq: 5},
			{Key: "e", State: "done", seq: 4},
			{Key: "f", State: "done", seq: 1},
		}
	}
	keys := func(items []item) (s string) {
		for _, it := range items {
			if it.More {
				s += "(" + it.Key + ")"
			} else {
				s += it.Key
			}
		}
		return s
	}
	for _, c := range []struct {
		order, side string
		group       bool
		doneMax     int
		want        string
	}{
		{"alpha", "right", false, -1, "abcdef"}, // as before
		{"recent", "right", false, -1, "bfcaed"},
		{"alpha", "right", true, -1, "aefdcb"}, // idle, working, done to see, waiting
		{"recent", "right", true, -1, "faedcb"},
		{"alpha", "right", true, 1, "(a)e(f)dcb"}, // the most recently changed idle stays out
		{"alpha", "right", false, 0, "(a)bcd(e)(f)"},
		{"recent", "right", true, 5, "faedcb"},
		{"alpha", "left", false, -1, "abcdef"}, // alphabetical still reads left to right
		{"recent", "left", false, -1, "deacbf"},
		{"alpha", "left", true, -1, "bcdaef"}, // waiting, done to see, working, idle
		{"recent", "left", true, 1, "bcde(a)(f)"},
	} {
		if got := keys(arrange(in(), c.order, c.side, c.group, c.doneMax)); got != c.want {
			t.Errorf("%s %s group=%v doneMax=%d: %s, want %s", c.order, c.side, c.group, c.doneMax, got, c.want)
		}
	}
}

func TestTrackerChanged(t *testing.T) {
	tr := newTracker()
	snap := func(states ...string) snapshot {
		var s snapshot
		for i, st := range states {
			s.Agents = append(s.Agents, agent{Key: string(rune('a' + i)), State: st})
		}
		return s
	}
	never := func(agent) bool { return false }
	tr.update(snap("working", "working"), never)
	items := tr.update(snap("working", "done"), never)
	if items[0].seq != 1 || items[1].seq != 2 {
		t.Errorf("seq: %d %d, want 1 2", items[0].seq, items[1].seq)
	}
}
