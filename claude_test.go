package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeHooksInstallRemove(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("APPDATA", dir) // removeClaudeHooks deletes the sessions dir under UserConfigDir
	t.Setenv("HOME", dir)
	path := filepath.Join(dir, "settings.json")
	orig := `{
  "permissions": {"allow": ["Bash(ls:*)"]},
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "C:/orca/claude-hook.cmd || echo {}"}]}],
    "PreCompact": [{"matcher": "auto", "hooks": [{"type": "command", "command": "notify.sh"}]}]
  },
  "effortLevel": "high"
}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if claudeHooksStatus() != hooksAbsent {
		t.Fatal("status before install")
	}

	if err := installClaudeHooks(); err != nil {
		t.Fatal(err)
	}
	installed, _ := os.ReadFile(path)
	if claudeHooksStatus() != hooksOK {
		t.Fatalf("status after install:\n%s", installed)
	}
	top, _ := decodeObject(installed)
	var keys []string
	for _, f := range top {
		keys = append(keys, f.Key)
	}
	if strings.Join(keys, ",") != "permissions,hooks,effortLevel" {
		t.Errorf("key order changed: %v", keys)
	}
	if !strings.Contains(string(installed), "C:/orca/claude-hook.cmd") || !strings.Contains(string(installed), "notify.sh") {
		t.Error("other hooks lost")
	}
	if n := strings.Count(string(installed), hookSuffix); n != len(hookEvents) {
		t.Errorf("%d lollipop entries, want %d", n, len(hookEvents))
	}
	if bak, _ := os.ReadFile(path + ".lollipop-bak"); string(bak) != orig {
		t.Error("backup is not the original file")
	}

	if err := installClaudeHooks(); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(path); !bytes.Equal(again, installed) {
		t.Error("second install changed the file")
	}

	if err := removeClaudeHooks(); err != nil {
		t.Fatal(err)
	}
	removed, _ := os.ReadFile(path)
	if claudeHooksStatus() != hooksAbsent || !bytes.Equal(compact(string(removed)), compact(orig)) {
		t.Errorf("remove didn't restore the original:\n%s", removed)
	}
}

func TestClaudeHooksInvalidSettingsUntouched(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path := filepath.Join(dir, "settings.json")
	_ = os.WriteFile(path, []byte(`{"hooks": broken`), 0o644)
	if err := installClaudeHooks(); err == nil {
		t.Error("install on invalid settings.json must fail")
	}
	if raw, _ := os.ReadFile(path); string(raw) != `{"hooks": broken` {
		t.Error("invalid settings.json was modified")
	}
}

func compact(s string) []byte {
	var b bytes.Buffer
	_ = json.Compact(&b, []byte(s))
	return b.Bytes()
}

func TestTranscriptTitle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	_ = os.WriteFile(path, []byte(`{"type":"ai-title","aiTitle":"Old"}
{"type":"user","message":"mentions \"ai-title\" in text"}
{"type":"ai-title","aiTitle":"New"}
{"type":"assistant"`), 0o644)
	if got := transcriptTitle(path); got != "New" {
		t.Errorf("title = %q", got)
	}
	if transcriptTitle(filepath.Join(t.TempDir(), "missing.jsonl")) != "" {
		t.Error("missing transcript must give no title")
	}
}

func TestSessionLink(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("HOME", dir)
	cfg, _ := os.UserConfigDir()
	sessions := filepath.Join(cfg, "Claude", "claude-code-sessions", "acct", "org")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"local_old.json":   `{"sessionId":"local_old","cliSessionId":"cli-1","lastActivityAt":1}`,
		"local_new.json":   `{"sessionId":"local_new","cliSessionId":"cli-1","lastActivityAt":2}`,
		"local_other.json": `{"sessionId":"local_other","cliSessionId":"cli-2","lastActivityAt":3}`,
		"local_bad.json":   `{"sessionId":"../x","cliSessionId":"cli-3","lastActivityAt":4}`,
	} {
		if err := os.WriteFile(filepath.Join(sessions, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		h    hostRef
		want string
	}{
		{hostRef{Entrypoint: "cli", Session: "cli-1"}, ""},
		{hostRef{Session: "cli-1"}, "claude://code/continue?session=local_new"}, // entrypoint recorded by an older lollipop
		{hostRef{Entrypoint: "claude-vscode", Session: "cli-1"}, "vscode://anthropic.claude-code/open?session=cli-1"},
		{hostRef{Entrypoint: "claude-desktop", Session: "cli-1"}, "claude://code/continue?session=local_new"},
		{hostRef{Entrypoint: "claude-desktop", Session: "cli-3"}, ""},
		{hostRef{Entrypoint: "claude-desktop", Session: "cli-9"}, ""},
	} {
		if got := sessionLink(c.h); got != c.want {
			t.Errorf("sessionLink(%+v) = %q, want %q", c.h, got, c.want)
		}
	}
}
