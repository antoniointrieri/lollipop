package main

// Claude Code sessions outside Orca (docs/spec.md §9). A command hook in the user's Claude Code settings runs
// "lollipop hook" on every event; it writes the session state to <UserConfigDir>/lollipop/claude/<id>.json,
// which the app reads on every poll.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var hookEvents = []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure",
	"PermissionRequest", "Notification", "Stop", "StopFailure", "SessionEnd"}

const hookSuffix = " hook || echo {}" // if the exe is gone, "{}" means no effect (same trick as Orca)

type claudeSession struct {
	State       string
	Cwd         string
	Title       string // conversation title, from the transcript
	ClaudePID   int
	ClaudeStart int64 // detects recycled pids
	Host        hostRef
	At          int64 // UnixNano; drops events that arrive out of order
}

// hostRef is the app window hosting a session (terminal, IDE, ...).
type hostRef struct {
	PID    int
	HWND   uintptr // exact window when known (consoles), else 0: pick among PID's windows
	Name   string  // executable name, e.g. "WindowsTerminal"
	Folder string  // session folder, to pick the right IDE window
}

var sessionIDRe = regexp.MustCompile(`^[A-Za-z0-9-]{1,100}$`)

func claudeSessionsDir() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "lollipop", "claude")
}

// hookState returns "" for events to ignore and "end" for a closed session.
func hookState(event, notificationType string) string {
	switch event {
	case "SessionStart", "Stop":
		return "done"
	case "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure":
		return "working"
	case "PermissionRequest":
		return "waiting"
	case "Notification":
		if notificationType == "permission_prompt" || notificationType == "elicitation_dialog" {
			return "waiting"
		}
	case "StopFailure":
		return "blocked"
	case "SessionEnd":
		return "end"
	}
	return ""
}

// runHook must never get in Claude's way: it always exits 0 (exit code 2 would block the action)
// and never writes to stdout.
func runHook() {
	defer func() {
		recover()
		os.Exit(0)
	}()
	at := time.Now().UnixNano()
	raw, _ := io.ReadAll(os.Stdin) // drain it fully: Claude expects the input to be consumed
	if os.Getenv("ORCA_PANE_KEY") != "" {
		return // Orca already shows it
	}
	var in struct {
		SessionID        string `json:"session_id"`
		Cwd              string `json:"cwd"`
		Transcript       string `json:"transcript_path"`
		Event            string `json:"hook_event_name"`
		NotificationType string `json:"notification_type"`
	}
	if json.Unmarshal(raw, &in) != nil || !sessionIDRe.MatchString(in.SessionID) {
		return
	}
	state := hookState(in.Event, in.NotificationType)
	file := filepath.Join(claudeSessionsDir(), in.SessionID+".json")
	switch state {
	case "":
		return
	case "end":
		os.Remove(file)
		return
	}
	var s claudeSession
	if old, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(old, &s)
	}
	if s.At > at {
		return // a newer event was already written (hooks can overlap)
	}
	// Refreshed on every event (the hook runs async, so it costs Claude nothing): picks up resumed sessions and
	// fixes a wrong host recorded by an older lollipop.
	// ponytail: without CLAUDE_PID (old Claude Code) there is no liveness check; the entry goes only on SessionEnd
	s.ClaudePID, _ = strconv.Atoi(os.Getenv("CLAUDE_PID"))
	s.ClaudeStart = processStart(s.ClaudePID)
	s.Host = findHost(s.ClaudePID)
	s.State, s.Cwd, s.At = state, in.Cwd, at
	if t := transcriptTitle(in.Transcript); t != "" {
		s.Title = t
	}
	s.Host.Folder = filepath.Base(in.Cwd)
	out, _ := json.Marshal(s)
	_ = writeAtomic(file, out)
}

// transcriptTitle returns the latest "ai-title" record near the end of the transcript.
// ponytail: internal Claude Code format; if it changes, only the title in the tooltip is lost
func transcriptTitle(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	const tail = 256 << 10
	if st, err := f.Stat(); err == nil && st.Size() > tail {
		f.Seek(st.Size()-tail, io.SeekStart)
	}
	data, _ := io.ReadAll(f)
	lines := bytes.Split(data, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if !bytes.Contains(lines[i], []byte(`"ai-title"`)) {
			continue
		}
		var r struct{ Type, AITitle string }
		if json.Unmarshal(lines[i], &r) == nil && r.Type == "ai-title" && r.AITitle != "" {
			return r.AITitle
		}
	}
	return ""
}

func writeAtomic(file string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp := file + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// loadClaudeSessions also deletes the files of dead sessions.
func loadClaudeSessions() []agent {
	dir := claudeSessionsDir()
	entries, _ := os.ReadDir(dir)
	var agents []agent
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		file := filepath.Join(dir, e.Name())
		var s claudeSession
		if raw, err := os.ReadFile(file); err != nil || json.Unmarshal(raw, &s) != nil {
			continue
		}
		if s.ClaudePID != 0 {
			if st := processStart(s.ClaudePID); st == 0 || (s.ClaudeStart != 0 && st != s.ClaudeStart) {
				os.Remove(file) // closed without SessionEnd (Ctrl+C, window closed, crash)
				continue
			}
		} else if time.Since(time.Unix(0, s.At)) > 12*time.Hour {
			os.Remove(file)
			continue
		}
		host := s.Host
		title := "Claude Code in " + hostLabel(host.Name)
		if s.Title != "" {
			title = s.Title + "\n" + title
		}
		agents = append(agents, agent{Key: "claude:" + id, State: s.State, Label: filepath.Base(s.Cwd), Title: title, Host: &host})
	}
	return agents
}

func hostLabel(exe string) string {
	names := map[string]string{"WindowsTerminal": "Windows Terminal", "Code": "VS Code", "idea64": "IntelliJ IDEA",
		"powershell": "PowerShell", "pwsh": "PowerShell", "cmd": tr("Command Prompt", "Prompt dei comandi"), "conhost": "console", "warp": "Warp"}
	if n, ok := names[exe]; ok {
		return n
	}
	if exe == "" {
		return tr("a terminal", "un terminale")
	}
	return exe
}

// --- Hook installation in the user's Claude Code settings ---

func claudeConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".claude")
}

func claudeSettingsPath() string { return filepath.Join(claudeConfigDir(), "settings.json") }

func hookCommand() string {
	exe, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	p := filepath.ToSlash(exe)
	if strings.Contains(p, " ") {
		p = `"` + p + `"`
	}
	return p + hookSuffix
}

// isOurHook matches "<path to a lollipop* executable> hook || echo {}".
func isOurHook(cmd string) bool {
	p, ok := strings.CutSuffix(cmd, hookSuffix)
	return ok && strings.HasPrefix(strings.ToLower(filepath.Base(strings.Trim(p, `"`))), "lollipop")
}

// A JSON object that keeps key order and leaves values untouched, so edits don't reshuffle the user's file.
type jsonField struct {
	Key string
	Val json.RawMessage
}

func decodeObject(raw []byte) ([]jsonField, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	var fields []jsonField
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		fields = append(fields, jsonField{t.(string), v})
	}
	return fields, nil
}

func encodeObject(fields []jsonField) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(f.Key)
		b.Write(k)
		b.WriteByte(':')
		b.Write(f.Val)
	}
	b.WriteByte('}')
	return b.Bytes()
}

func field(fields []jsonField, key string) (json.RawMessage, int) {
	for i, f := range fields {
		if f.Key == key {
			return f.Val, i
		}
	}
	return nil, -1
}

// withoutOurHooks drops lollipop entries from an event's groups ({matcher, hooks}); groups without our
// entries stay byte for byte. It also returns the commands it removed.
func withoutOurHooks(groups []json.RawMessage) (kept []json.RawMessage, ours []string, err error) {
	for _, g := range groups {
		gf, err := decodeObject(g)
		if err != nil {
			return nil, nil, err
		}
		hv, hi := field(gf, "hooks")
		var hooks []json.RawMessage
		if hi < 0 || json.Unmarshal(hv, &hooks) != nil {
			kept = append(kept, g)
			continue
		}
		var rest []json.RawMessage
		for _, h := range hooks {
			var c struct{ Command string }
			if json.Unmarshal(h, &c) == nil && isOurHook(c.Command) {
				ours = append(ours, c.Command)
			} else {
				rest = append(rest, h)
			}
		}
		switch {
		case len(rest) == len(hooks):
			kept = append(kept, g)
		case len(rest) > 0:
			gf[hi].Val, _ = json.Marshal(rest)
			kept = append(kept, encodeObject(gf))
		}
	}
	return kept, ours, nil
}

// editClaudeHooks removes lollipop entries and, if install, adds them back with the current exe path.
// It writes nothing when nothing changes and backs the file up before writing.
func editClaudeHooks(install bool) error {
	path := claudeSettingsPath()
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if !install {
			return nil
		}
		raw = []byte("{}")
	} else if err != nil {
		return err
	}
	top, err := decodeObject(raw)
	if err != nil {
		return errors.New(tr("Claude Code settings.json is not valid JSON: not touching it", "settings.json di Claude Code non valido: non lo modifico"))
	}
	var hooks []jsonField
	hv, hi := field(top, "hooks")
	if hi >= 0 {
		if hooks, err = decodeObject(hv); err != nil {
			return errors.New(tr("settings.json: invalid \"hooks\", not touching it", "settings.json: \"hooks\" non valido, non lo modifico"))
		}
	}
	for _, ev := range hookEvents {
		gv, gi := field(hooks, ev)
		var groups []json.RawMessage
		if gi >= 0 {
			if err := json.Unmarshal(gv, &groups); err != nil {
				return errors.New(tr("settings.json: invalid hook ", "settings.json: hook non valido ") + ev)
			}
			if groups, _, err = withoutOurHooks(groups); err != nil {
				return err
			}
		}
		if install {
			// async: Claude doesn't wait for us; process start on Windows alone costs ~200 ms
			ours, _ := json.Marshal(map[string]any{"hooks": []map[string]any{{"type": "command", "command": hookCommand(), "async": true}}})
			groups = append(groups, ours)
		}
		switch {
		case len(groups) > 0 && gi >= 0:
			hooks[gi].Val, _ = json.Marshal(groups)
		case len(groups) > 0:
			v, _ := json.Marshal(groups)
			hooks = append(hooks, jsonField{ev, v})
		case gi >= 0:
			hooks = append(hooks[:gi], hooks[gi+1:]...)
		}
	}
	switch {
	case len(hooks) > 0 && hi >= 0:
		top[hi].Val = encodeObject(hooks)
	case len(hooks) > 0:
		top = append(top, jsonField{"hooks", encodeObject(hooks)})
	case hi >= 0:
		top = append(top[:hi], top[hi+1:]...)
	}
	var out bytes.Buffer
	if err := json.Indent(&out, encodeObject(top), "", "  "); err != nil {
		return err
	}
	out.WriteByte('\n')
	var before bytes.Buffer
	if json.Indent(&before, raw, "", "  ") == nil && bytes.Equal(bytes.TrimSpace(before.Bytes()), bytes.TrimSpace(out.Bytes())) {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		if err := os.WriteFile(path+".lollipop-bak", raw, 0o644); err != nil {
			return err
		}
	}
	return writeAtomic(path, out.Bytes())
}

func installClaudeHooks() error { return editClaudeHooks(true) }

// removeClaudeHooks also deletes recorded sessions, which would no longer be updated.
func removeClaudeHooks() error {
	if err := editClaudeHooks(false); err != nil {
		return err
	}
	return os.RemoveAll(claudeSessionsDir())
}

type hooksStatus int

const (
	hooksAbsent  hooksStatus = iota
	hooksOK                  // one entry per event, with the current path
	hooksBroken              // partial, or with an old path
	hooksInvalid             // unreadable settings.json
)

func claudeHooksStatus() hooksStatus {
	raw, err := os.ReadFile(claudeSettingsPath())
	if errors.Is(err, os.ErrNotExist) {
		return hooksAbsent
	}
	top, err := decodeObject(raw)
	if err != nil {
		return hooksInvalid
	}
	hv, hi := field(top, "hooks")
	if hi < 0 {
		return hooksAbsent
	}
	hooks, err := decodeObject(hv)
	if err != nil {
		return hooksInvalid
	}
	found, good := 0, 0
	for _, ev := range hookEvents {
		var groups []json.RawMessage
		gv, _ := field(hooks, ev)
		_ = json.Unmarshal(gv, &groups)
		_, ours, _ := withoutOurHooks(groups)
		found += len(ours)
		if len(ours) == 1 && ours[0] == hookCommand() {
			good++
		}
	}
	switch {
	case found == 0:
		return hooksAbsent
	case good == len(hookEvents) && found == good:
		return hooksOK
	}
	return hooksBroken
}
