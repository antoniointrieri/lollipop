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
