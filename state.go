package main

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// Orca responses: only the fields we use (encoding/json matches names case-insensitively).
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
	Host                             *hostRef // set only for Claude Code sessions outside Orca
}

type snapshot struct {
	Agents  []agent
	Focused string // paneKey of the pane in front inside Orca
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
		if !w.IsMainWorktree { // "repo/name" tells worktrees of the same repo apart
			label += "/" + w.DisplayName
		}
		for _, a := range w.Agents {
			i, ok := terms[a.PaneKey]
			if !ok { // "done" without a terminal is a closed session
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
	sortAgents(s.Agents)
	// Pane in front = active worktree -> its active tab -> its active leaf.
	// ponytail: root group only; with tabs split into several groups it picks the root one
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

func sortAgents(agents []agent) {
	sort.SliceStable(agents, func(i, j int) bool {
		a, b := strings.ToLower(agents[i].Label), strings.ToLower(agents[j].Label)
		if a != b {
			return a < b
		}
		return agents[i].Key < agents[j].Key
	})
}

// stripGlyph removes the leading status glyph from a terminal title ("✳ Claude Code" -> "Claude Code").
func stripGlyph(title string) string {
	return strings.TrimLeftFunc(title, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
}

type item struct {
	Key    string `json:"key"`
	Handle string `json:"handle"`
	Label  string `json:"label"`
	State  string `json:"state"`
	Tip    string `json:"tip"`
	Blink  bool   `json:"blink"`
}

// tracker remembers the last state of each agent and which ones finished but weren't looked at yet.
type tracker struct {
	mu    sync.Mutex
	prev  map[string]string
	blink map[string]bool
}

func newTracker() *tracker { return &tracker{prev: map[string]string{}, blink: map[string]bool{}} }

// update starts blinking on a working -> other transition observed here (nothing blinks at startup) and stops
// when the agent works again or the user is looking at it; seen is called only for blinking agents.
func (t *tracker) update(s snapshot, seen func(agent) bool) []item {
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
	for _, a := range s.Agents {
		if t.blink[a.Key] && seen(a) {
			delete(t.blink, a.Key)
		}
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

// summary returns the tray icon color (most urgent agent) and the tooltip text.
func summary(items []item, errMsg string) (color, tip string) {
	if errMsg != "" {
		tip = "lollipop — " + tr("Error: ", "Errore: ") + errMsg
		if r := []rune(tip); len(r) > 120 { // Windows tray tooltips hold 127 chars
			tip = string(r[:120]) + "…"
		}
		return "red", tip
	}
	if len(items) == 0 {
		return "gray", "lollipop — " + tr("no active agents", "nessun agente attivo")
	}
	count := map[string]int{}
	for _, it := range items {
		switch it.State {
		case "working", "done", "monitoring":
			count[it.State]++
		default:
			count["waiting"]++
		}
	}
	var parts []string
	for _, g := range []struct{ state, color, label string }{ // most urgent first
		{"waiting", "red", tr("waiting", "in attesa")}, {"done", "green", "done"}, {"monitoring", "blue", "monitoring"}, {"working", "yellow", "working"},
	} {
		if n := count[g.state]; n > 0 {
			if color == "" {
				color = g.color
			}
			parts = append(parts, strconv.Itoa(n)+" "+g.label)
		}
	}
	return color, "lollipop — " + strings.Join(parts, ", ")
}
