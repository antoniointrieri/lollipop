package main

import (
	"sort"
	"strings"
	"sync"
	"unicode"
)

// Risposte di Orca (solo i campi usati; json li abbina ignorando le maiuscole).
type worktreePS struct {
	Worktrees []struct {
		WorktreeID, Repo, DisplayName string
		IsMainWorktree, IsActive      bool
		Agents                        []struct{ PaneKey, State string }
	}
}

type terminalList struct {
	Terminals     []struct{ Handle, TabID, LeafID, Title string }
	VisualLayouts []struct {
		WorktreeID string
		Root       struct {
			ActiveTabID string
			Tabs        []struct{ TabID, ActiveLeafID string }
		}
	}
}

type lastStatus struct {
	Entries map[string]struct {
		Payload struct{ WorkingMode string }
	}
}

type agent struct {
	Key, Handle, State, Label, Title string
}

type snapshot struct {
	Agents  []agent
	Focused string // paneKey del pannello in primo piano dentro Orca
}

func buildSnapshot(wt worktreePS, tl terminalList, hooks lastStatus) snapshot {
	terms := map[string]int{}
	for i, t := range tl.Terminals {
		terms[t.TabID+":"+t.LeafID] = i
	}
	var s snapshot
	activeWT := ""
	for _, w := range wt.Worktrees {
		if w.IsActive && activeWT == "" {
			activeWT = w.WorktreeID
		}
		label := w.Repo
		if !w.IsMainWorktree { // "repo/nome": due worktree dello stesso repo si distinguono
			label += "/" + w.DisplayName
		}
		for _, a := range w.Agents {
			i, ok := terms[a.PaneKey]
			if !ok { // "done" senza terminale = sessione chiusa
				continue
			}
			state := a.State
			if state == "working" && hooks.Entries[a.PaneKey].Payload.WorkingMode == "monitoring" {
				state = "monitoring"
			}
			t := tl.Terminals[i]
			s.Agents = append(s.Agents, agent{Key: a.PaneKey, Handle: t.Handle, State: state, Label: label, Title: stripGlyph(t.Title)})
		}
	}
	sort.SliceStable(s.Agents, func(i, j int) bool {
		a, b := strings.ToLower(s.Agents[i].Label), strings.ToLower(s.Agents[j].Label)
		if a != b {
			return a < b
		}
		return s.Agents[i].Key < s.Agents[j].Key
	})
	// Pannello in primo piano = worktree attivo -> sua scheda attiva -> suo pannello attivo.
	// ponytail: legge solo il gruppo radice; con le schede divise in piu' gruppi prende quello radice
	for _, l := range tl.VisualLayouts {
		if l.WorktreeID != activeWT || activeWT == "" {
			continue
		}
		for _, tab := range l.Root.Tabs {
			if tab.TabID == l.Root.ActiveTabID && tab.ActiveLeafID != "" {
				s.Focused = tab.TabID + ":" + tab.ActiveLeafID
			}
		}
	}
	return s
}

// Toglie il glifo iniziale dal titolo del terminale (es. "✳ Claude Code" -> "Claude Code").
func stripGlyph(title string) string {
	return strings.TrimLeftFunc(title, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
}

// Voce mostrata dal frontend.
type item struct {
	Key    string `json:"key"`
	Handle string `json:"handle"`
	Label  string `json:"label"`
	State  string `json:"state"`
	Tip    string `json:"tip"`
	Blink  bool   `json:"blink"`
}

// tracker ricorda gli stati visti e quali agenti sono "appena finiti, non ancora visti".
type tracker struct {
	mu    sync.Mutex
	prev  map[string]string
	blink map[string]bool
}

func newTracker() *tracker { return &tracker{prev: map[string]string{}, blink: map[string]bool{}} }

// update: lampeggia solo sulla transizione working -> altro osservata qui (all'avvio nessuno lampeggia);
// smette se torna working o se Orca e' in primo piano con quel pannello attivo (orcaInFront chiamata solo se serve).
func (t *tracker) update(s snapshot, orcaInFront func() bool) []item {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, a := range s.Agents {
		if t.prev[a.Key] == "working" && a.State != "working" {
			t.blink[a.Key] = true
		}
		if a.State == "working" {
			delete(t.blink, a.Key)
		}
		t.prev[a.Key] = a.State
	}
	if t.blink[s.Focused] && orcaInFront() {
		delete(t.blink, s.Focused)
	}
	items := []item{}
	for _, a := range s.Agents {
		items = append(items, item{Key: a.Key, Handle: a.Handle, Label: a.Label, State: a.State,
			Tip: "[" + a.State + "] " + a.Label + "\n" + a.Title, Blink: t.blink[a.Key]})
	}
	return items
}

func (t *tracker) seen(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.blink, key)
}
