package main

import (
	"bytes"
	"encoding/json"
	"image/color"
	"image/png"
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
	if len(s.Agents) != 2 { // t9:l9 senza terminale: sessione chiusa, ignorata
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
	blink := func(s snapshot) bool { return tr.update(s, func() bool { return front })[0].Blink }

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
	if c, tip := summary(items, ""); c != "red" || tip != "lollipop — 1 in attesa, 1 done, 2 working" {
		t.Errorf("%s %q", c, tip)
	}
	if c, _ := summary(items[:3], ""); c != "green" {
		t.Errorf("senza attese: %s", c)
	}
	if c, tip := summary(nil, ""); c != "gray" || tip != "lollipop — nessun agente attivo" {
		t.Errorf("%s %q", c, tip)
	}
	if c, _ := summary(items, "pipe chiusa"); c != "red" {
		t.Errorf("errore: %s", c)
	}
}

func TestLollipopPNG(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(lollipopPNG(color.RGBA{255, 0, 0, 255})))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := img.At(0, 31).RGBA(); a != 0 {
		t.Error("angolo in basso a sinistra: deve essere trasparente")
	}
	if r, _, _, a := img.At(2, 13).RGBA(); a>>8 != 255 || r>>8 != 255 {
		t.Errorf("testa: deve essere rossa e opaca, e' r=%d a=%d", r>>8, a>>8)
	}
	white := false
	for y := 5; y < 22; y++ {
		for x := 5; x < 22; x++ {
			if _, g, _, _ := img.At(x, y).RGBA(); g>>8 > 200 {
				white = true
			}
		}
	}
	if !white {
		t.Error("vortice bianco mancante")
	}
	if _, _, _, a := img.At(27, 27).RGBA(); a == 0 {
		t.Error("bastoncino mancante")
	}
}
