package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// writePlugin creates an executable script and its JSON manifest in dir.
func writePlugin(t *testing.T, dir, stem, script, manifest string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, stem), []byte(script), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, stem+".json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

// ── LoadPlugins ───────────────────────────────────────────────────────────────

func TestLoadPlugins_NoDir(t *testing.T) {
	got, err := LoadPlugins("/nonexistent/feino/plugins/xyz", nil)
	if err != nil {
		t.Fatalf("expected nil error for missing dir, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice for missing dir, got %d tools", len(got))
	}
}

func TestLoadPlugins_EmptyDir(t *testing.T) {
	got, err := LoadPlugins(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 tools in empty dir, got %d", len(got))
	}
}

func TestLoadPlugins_Valid(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script plugins require a Unix-like shell")
	}
	dir := t.TempDir()
	writePlugin(t, dir, "greet",
		"#!/bin/sh\necho '{\"content\":\"hello world\"}'",
		`{"name":"greet","description":"Says hello","permission_level":"read","parameters":{"type":"object","properties":{}}}`,
	)

	loaded, err := LoadPlugins(dir, nil)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(loaded))
	}
	p := loaded[0]
	if p.GetName() != "greet" {
		t.Errorf("name: got %q, want greet", p.GetName())
	}
	if p.GetDescription() != "Says hello" {
		t.Errorf("description: got %q", p.GetDescription())
	}

	result := p.Run(map[string]any{})
	if result.GetError() != nil {
		t.Fatalf("Run: unexpected error: %v", result.GetError())
	}
	if result.GetContent() != "hello world" {
		t.Errorf("content: got %q, want %q", result.GetContent(), "hello world")
	}
}

func TestLoadPlugins_NameFallsBackToStem(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script plugins require a Unix-like shell")
	}
	dir := t.TempDir()
	// Manifest omits the "name" field — stem should be used.
	writePlugin(t, dir, "my_tool",
		"#!/bin/sh\necho '{\"content\":\"ok\"}'",
		`{"description":"A tool","parameters":{"type":"object","properties":{}}}`,
	)

	loaded, err := LoadPlugins(dir, nil)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(loaded))
	}
	if loaded[0].GetName() != "my_tool" {
		t.Errorf("name: got %q, want my_tool", loaded[0].GetName())
	}
}

func TestLoadPlugins_MissingExecutable(t *testing.T) {
	dir := t.TempDir()
	// Write manifest only — no executable counterpart.
	if err := os.WriteFile(
		filepath.Join(dir, "orphan.json"),
		[]byte(`{"name":"orphan","description":"","parameters":{}}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadPlugins(dir, nil)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("expected 0 plugins when executable is missing, got %d", len(loaded))
	}
}

func TestLoadPlugins_BadManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script plugins require a Unix-like shell")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken"), []byte("#!/bin/sh\necho ok"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadPlugins(dir, nil)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("expected 0 plugins for bad manifest, got %d", len(loaded))
	}
}

func TestLoadPlugins_NoExecBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable bits are Unix-only")
	}
	dir := t.TempDir()
	// File exists but lacks the executable bit.
	if err := os.WriteFile(filepath.Join(dir, "notexec"), []byte("#!/bin/sh\necho ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "notexec.json"),
		[]byte(`{"name":"notexec","description":"","parameters":{}}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadPlugins(dir, nil)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("expected 0 plugins when exec bit is missing, got %d", len(loaded))
	}
}

func TestLoadPlugins_ExtensionMatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script plugins require a Unix-like shell")
	}
	dir := t.TempDir()
	// Plugin executable carries a .sh extension — still matches the manifest stem.
	if err := os.WriteFile(filepath.Join(dir, "ext_tool.sh"), []byte("#!/bin/sh\necho '{\"content\":\"from sh\"}'"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "ext_tool.json"),
		[]byte(`{"name":"ext_tool","description":"","parameters":{}}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadPlugins(dir, nil)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 plugin (with .sh extension), got %d", len(loaded))
	}
	result := loaded[0].Run(map[string]any{})
	if result.GetError() != nil {
		t.Fatalf("Run: %v", result.GetError())
	}
	if result.GetContent() != "from sh" {
		t.Errorf("content: got %q, want from sh", result.GetContent())
	}
}

func TestLoadPlugins_DefaultTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script plugins require a Unix-like shell")
	}
	dir := t.TempDir()
	writePlugin(t, dir, "notimeout",
		"#!/bin/sh\necho '{\"content\":\"ok\"}'",
		`{"name":"notimeout","description":"","parameters":{}}`,
	)
	loaded, _ := LoadPlugins(dir, nil)
	if sp, ok := loaded[0].(*ScriptPlugin); ok {
		if sp.timeout != 30*time.Second {
			t.Errorf("default timeout: got %s, want 30s", sp.timeout)
		}
	}
}

func TestLoadPlugins_CustomTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script plugins require a Unix-like shell")
	}
	dir := t.TempDir()
	writePlugin(t, dir, "fasttool",
		"#!/bin/sh\necho '{\"content\":\"ok\"}'",
		`{"name":"fasttool","description":"","parameters":{},"timeout_seconds":5}`,
	)
	loaded, _ := LoadPlugins(dir, nil)
	if sp, ok := loaded[0].(*ScriptPlugin); ok {
		if sp.timeout != 5*time.Second {
			t.Errorf("custom timeout: got %s, want 5s", sp.timeout)
		}
	}
}

// ── ScriptPlugin.Run ─────────────────────────────────────────────────────────

func TestScriptPlugin_Run_PlainText(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script plugins require a Unix-like shell")
	}
	dir := t.TempDir()
	writePlugin(t, dir, "plain",
		"#!/bin/sh\necho 'plain text output'",
		`{"name":"plain","description":"","parameters":{}}`,
	)
	loaded, _ := LoadPlugins(dir, nil)
	result := loaded[0].Run(map[string]any{})
	if result.GetError() != nil {
		t.Fatalf("unexpected error: %v", result.GetError())
	}
	if result.GetContent() != "plain text output" {
		t.Errorf("content: got %q, want %q", result.GetContent(), "plain text output")
	}
}

func TestScriptPlugin_Run_IsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script plugins require a Unix-like shell")
	}
	dir := t.TempDir()
	writePlugin(t, dir, "errtool",
		"#!/bin/sh\nprintf '{\"content\":\"something went wrong\",\"is_error\":true}'",
		`{"name":"errtool","description":"","parameters":{}}`,
	)
	loaded, _ := LoadPlugins(dir, nil)
	result := loaded[0].Run(map[string]any{})
	if result.GetError() == nil {
		t.Fatal("expected error from is_error response, got nil")
	}
	if result.GetContent() != "something went wrong" {
		t.Errorf("content: got %q, want %q", result.GetContent(), "something went wrong")
	}
}

func TestScriptPlugin_Run_PassesParams(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script plugins require a Unix-like shell")
	}
	dir := t.TempDir()
	// The script reads its stdin as a JSON object and echoes the "msg" field.
	writePlugin(t, dir, "echo_msg",
		`#!/bin/sh
read input
msg=$(printf '%s' "$input" | grep -o '"msg":"[^"]*"' | cut -d'"' -f4)
printf '{"content":"%s"}' "$msg"`,
		`{"name":"echo_msg","description":"","parameters":{"type":"object","properties":{"msg":{"type":"string"}},"required":["msg"]}}`,
	)
	loaded, _ := LoadPlugins(dir, nil)
	result := loaded[0].Run(map[string]any{"msg": "ping"})
	if result.GetError() != nil {
		t.Fatalf("unexpected error: %v", result.GetError())
	}
	if result.GetContent() != "ping" {
		t.Errorf("content: got %q, want ping", result.GetContent())
	}
}

func TestScriptPlugin_Run_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script plugins require a Unix-like shell")
	}
	dir := t.TempDir()
	writePlugin(t, dir, "slow",
		"#!/bin/sh\nsleep 60",
		`{"name":"slow","description":"","parameters":{}}`,
	)
	loaded, _ := LoadPlugins(dir, nil)

	// Override the loaded timeout so the test completes quickly.
	sp := loaded[0].(*ScriptPlugin)
	sp.timeout = 100 * time.Millisecond

	start := time.Now()
	result := loaded[0].Run(map[string]any{})
	elapsed := time.Since(start)

	if result.GetError() == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if elapsed > 5*time.Second {
		t.Errorf("timeout did not fire fast enough: %s", elapsed)
	}
}

// ── permLevelFromString ───────────────────────────────────────────────────────

func TestPermLevelFromString(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"read", PermLevelRead},
		{"write", PermLevelWrite},
		{"bash", PermLevelBash},
		{"danger_zone", PermLevelDangerZone},
		{"", PermLevelRead},        // empty → read
		{"unknown", PermLevelRead}, // unknown → read
	}
	for _, tc := range cases {
		if got := permLevelFromString(tc.in); got != tc.want {
			t.Errorf("permLevelFromString(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// ── interface compliance ──────────────────────────────────────────────────────

func TestScriptPlugin_ImplementsInterfaces(t *testing.T) {
	var _ Tool = (*ScriptPlugin)(nil)
	var _ Classified = (*ScriptPlugin)(nil)
}
