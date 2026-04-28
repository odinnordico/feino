package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odinnordico/feino/internal/config"
)

func newTestModel(workingDir string) *Model {
	return &Model{
		cfg: &config.Config{
			Context: &config.ContextConfig{WorkingDir: workingDir},
		},
	}
}

// ── currentModelName ────────────────────────────────────────────────────────

func TestCurrentModelName_AnthropicWithKey(t *testing.T) {
	m := &Model{cfg: &config.Config{Providers: &config.ProvidersConfig{
		Anthropic: config.AnthropicConfig{APIKey: "sk-ant-x", DefaultModel: "claude-opus-4-6"},
		OpenAI:    config.OpenAIConfig{APIKey: "sk-oai-x", DefaultModel: "gpt-4o"},
	}}}
	// Anthropic is checked first and has credentials → should win.
	if got := m.currentModelName(); got != "claude-opus-4-6" {
		t.Errorf("got %q, want claude-opus-4-6", got)
	}
}

func TestCurrentModelName_SkipsUncredentialedProvider(t *testing.T) {
	m := &Model{cfg: &config.Config{Providers: &config.ProvidersConfig{
		// Anthropic has a model but no key → should be skipped.
		Anthropic: config.AnthropicConfig{DefaultModel: "claude-opus-4-6"},
		OpenAI:    config.OpenAIConfig{APIKey: "sk-oai-x", DefaultModel: "gpt-4o"},
	}}}
	if got := m.currentModelName(); got != "gpt-4o" {
		t.Errorf("got %q, want gpt-4o", got)
	}
}

func TestCurrentModelName_OllamaNoKey(t *testing.T) {
	m := &Model{cfg: &config.Config{Providers: &config.ProvidersConfig{
		Ollama: config.OllamaConfig{DefaultModel: "llama3"},
	}}}
	if got := m.currentModelName(); got != "llama3" {
		t.Errorf("got %q, want llama3", got)
	}
}

func TestCurrentModelName_NoneConfigured(t *testing.T) {
	m := &Model{}
	if got := m.currentModelName(); got != "no model" {
		t.Errorf("got %q, want 'no model'", got)
	}
}

func TestExpandAtRefs_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := newTestModel(dir)
	got := m.expandAtRefs("see @hello.txt please")
	if !strings.Contains(got, "hello world") {
		t.Errorf("expected file content in expansion, got: %q", got)
	}
	if !strings.Contains(got, "<file") {
		t.Errorf("expected <file> tag in expansion, got: %q", got)
	}
}

func TestExpandAtRefs_AbsolutePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "abs.txt")
	if err := os.WriteFile(path, []byte("absolute"), 0o600); err != nil {
		t.Fatal(err)
	}

	// WorkingDir is set to something else; absolute path should still resolve.
	m := newTestModel("/some/other/dir")
	got := m.expandAtRefs("read @" + path)
	if !strings.Contains(got, "absolute") {
		t.Errorf("expected content from absolute path, got: %q", got)
	}
}

func TestExpandAtRefs_MissingFile(t *testing.T) {
	m := newTestModel(t.TempDir())
	input := "see @nonexistent.txt"
	got := m.expandAtRefs(input)
	if got != input {
		t.Errorf("missing path should be left unchanged: got %q", got)
	}
}

func TestExpandAtRefs_FileTooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.bin")

	// Create a file just over the 20 MB limit.
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxAtRefFileSize + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	_ = f.Close()

	m := newTestModel(dir)
	input := "data @big.bin"
	got := m.expandAtRefs(input)
	if got != input {
		t.Errorf("oversized file should be left unchanged: got %q", got)
	}
}

func TestExpandAtRefs_Directory(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "sub")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "a.go"), []byte("package a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := newTestModel(filepath.Dir(dir))
	got := m.expandAtRefs("look at @" + filepath.Base(dir))
	if !strings.Contains(got, "<directory") {
		t.Errorf("expected <directory> tag, got: %q", got)
	}
	if !strings.Contains(got, "sub/") {
		t.Errorf("expected subdir in tree, got: %q", got)
	}
	if !strings.Contains(got, "a.go") {
		t.Errorf("expected a.go in tree, got: %q", got)
	}
	if !strings.Contains(got, "b.go") {
		t.Errorf("expected b.go in tree, got: %q", got)
	}
	if !strings.Contains(got, "rel:") || !strings.Contains(got, "abs:") {
		t.Errorf("expected rel: and abs: labels in tree, got: %q", got)
	}
}

func TestExpandAtRefs_DirectorySkipsGit(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := newTestModel(filepath.Dir(dir))
	got := m.expandAtRefs("@" + filepath.Base(dir))
	if strings.Contains(got, ".git") {
		t.Errorf(".git directory should be skipped, got: %q", got)
	}
}

func TestExpandAtRefs_MultipleTokens(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"x.txt", "y.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	m := newTestModel(dir)
	got := m.expandAtRefs("see @x.txt and @y.txt")
	if !strings.Contains(got, "x.txt") || !strings.Contains(got, "y.txt") {
		t.Errorf("expected both files expanded, got: %q", got)
	}
}

// ── routeStreamChunk ─────────────────────────────────────────────────────────

func TestRouteStreamChunk_NoThought(t *testing.T) {
	ws, th, in := routeStreamChunk("hello world", false)
	if ws != "hello world" || th != "" || in {
		t.Errorf("got ws=%q th=%q in=%v", ws, th, in)
	}
}

func TestRouteStreamChunk_FullThought(t *testing.T) {
	ws, th, in := routeStreamChunk("pre<thought>inner</thought>post", false)
	if ws != "prepost" || th != "inner" || in {
		t.Errorf("got ws=%q th=%q in=%v", ws, th, in)
	}
}

func TestRouteStreamChunk_OpenThought(t *testing.T) {
	// Chunk ends inside a <thought> block — should signal inThought=true.
	ws, th, in := routeStreamChunk("pre<thought>start of thought", false)
	if ws != "pre" || th != "start of thought" || !in {
		t.Errorf("got ws=%q th=%q in=%v", ws, th, in)
	}
}

func TestRouteStreamChunk_ContinueThought(t *testing.T) {
	// Continuation chunk — still inside thought.
	ws, th, in := routeStreamChunk("more thought", true)
	if ws != "" || th != "more thought" || !in {
		t.Errorf("got ws=%q th=%q in=%v", ws, th, in)
	}
}

func TestRouteStreamChunk_CloseThought(t *testing.T) {
	// Closing chunk — ends the thought block.
	ws, th, in := routeStreamChunk("end</thought>after", true)
	if ws != "after" || th != "end" || in {
		t.Errorf("got ws=%q th=%q in=%v", ws, th, in)
	}
}

func TestRouteStreamChunk_MultipleThoughts(t *testing.T) {
	ws, th, in := routeStreamChunk("a<thought>t1</thought>b<thought>t2</thought>c", false)
	if ws != "abc" || th != "t1t2" || in {
		t.Errorf("got ws=%q th=%q in=%v", ws, th, in)
	}
}
