package context

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odinnordico/feino/internal/tools"
)

type mockTool struct {
	name string
	desc string
}

func (m *mockTool) GetName() string               { return m.name }
func (m *mockTool) GetDescription() string        { return m.desc }
func (m *mockTool) GetParameters() map[string]any { return nil }
func (m *mockTool) Run(_ map[string]any) tools.ToolResult {
	return tools.NewToolResult(nil, nil)
}
func (m *mockTool) GetLogger() *slog.Logger { return slog.Default() }

func TestAssembleContext_PrioritiesAndBudget(t *testing.T) {
	tmpDir := t.TempDir()

	projectFile := filepath.Join(tmpDir, "FEINO.md")
	if err := os.WriteFile(projectFile, []byte("Project instructions content"), 0644); err != nil {
		t.Fatalf("failed to write project file: %v", err)
	}

	mgr := NewFileSystemContextManager(tmpDir)
	mgr.activeFile = "FEINO.md"
	mgr.globalConfigPath = "" // skip global for this test

	mgr.SetTools([]tools.Tool{
		&mockTool{name: "test_tool", desc: "does testing stuff"},
	})

	mgr.codeChunks = append(mgr.codeChunks, SemanticChunk{
		Name:     "MyFunc",
		Content:  "func MyFunc() { println(1) }",
		FilePath: "main.go",
	})

	// 1. Large budget — code chunks must appear.
	prompt, err := mgr.AssembleContext(context.Background(), 10000)
	if err != nil {
		t.Fatalf("AssembleContext failed: %v", err)
	}
	if !strings.Contains(prompt, "MyFunc") {
		t.Errorf("Missing code chunk in large-budget assembly")
	}

	// 2. Tiny budget — code chunks must be truncated.
	prompt, err = mgr.AssembleContext(context.Background(), 50)
	if err != nil {
		t.Fatalf("AssembleContext (small budget) failed: %v", err)
	}
	if strings.Contains(prompt, "MyFunc") {
		t.Errorf("Code chunk should have been truncated due to budget")
	}
}

func TestAutoDetect_Priority(t *testing.T) {
	tmpDir := t.TempDir()

	// No context file → AutoDetect must return false.
	mgr := NewFileSystemContextManager(tmpDir)
	if mgr.AutoDetect() {
		t.Fatal("expected false when no context file exists")
	}
	if mgr.GetActiveFile() != "" {
		t.Errorf("expected empty active file, got %q", mgr.GetActiveFile())
	}

	// Create CLAUDE.md (lowest priority).
	if err := os.WriteFile(filepath.Join(tmpDir, "CLAUDE.md"), []byte("claude"), 0644); err != nil {
		t.Fatal(err)
	}
	if !mgr.AutoDetect() {
		t.Fatal("expected true after writing CLAUDE.md")
	}
	if mgr.GetActiveFile() != "CLAUDE.md" {
		t.Errorf("expected CLAUDE.md, got %q", mgr.GetActiveFile())
	}

	// Add FEINO.md (highest priority) — must win.
	if err := os.WriteFile(filepath.Join(tmpDir, "FEINO.md"), []byte("feino"), 0644); err != nil {
		t.Fatal(err)
	}
	if !mgr.AutoDetect() {
		t.Fatal("expected true after writing FEINO.md")
	}
	if mgr.GetActiveFile() != "FEINO.md" {
		t.Errorf("expected FEINO.md to win, got %q", mgr.GetActiveFile())
	}
}

func TestAddCodeContext_AppendsChunks(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewFileSystemContextManager(tmpDir)
	mgr.activeFile = ""
	mgr.globalConfigPath = ""

	source := []byte("package main\n\nfunc Greet() {}\n")
	if err := mgr.AddCodeContext(context.Background(), "greet.go", source); err != nil {
		t.Fatalf("AddCodeContext failed: %v", err)
	}

	mgr.mu.RLock()
	n := len(mgr.codeChunks)
	mgr.mu.RUnlock()

	if n == 0 {
		t.Error("expected at least one chunk after AddCodeContext")
	}
}

func TestLoadSkills_DiscoverAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, ".feino", "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: deploy\ndescription: Deploy service.\n---\n\nRun deploy.\n"
	if err := os.WriteFile(filepath.Join(skillsDir, "deploy.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := NewFileSystemContextManager(tmpDir)
	if err := mgr.LoadSkills(); err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}

	mgr.mu.RLock()
	n := len(mgr.skills)
	mgr.mu.RUnlock()

	if n != 1 {
		t.Errorf("expected 1 skill, got %d", n)
	}
}

func TestLoadSkills_NoDir(t *testing.T) {
	mgr := NewFileSystemContextManager(t.TempDir())
	// No skills directory — should be a no-op, not an error.
	if err := mgr.LoadSkills(); err != nil {
		t.Fatalf("LoadSkills with no dir should not error: %v", err)
	}
}

func TestAppendLearning_Lifecycle(t *testing.T) {
	tmpDir := t.TempDir()

	path := filepath.Join(tmpDir, "FEINO.md")
	if err := os.WriteFile(path, []byte("# Project\n\n## Existing Section\nContent\n"), 0644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	mgr := NewFileSystemContextManager(tmpDir)
	mgr.activeFile = "FEINO.md"

	// 1. Append to non-existent section — should create the header.
	if err := mgr.AppendLearning("Use Go 1.26 features"); err != nil {
		t.Fatalf("AppendLearning failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file after first append: %v", err)
	}
	if !strings.Contains(string(data), "## Agent Learnings") {
		t.Errorf("Section header not created")
	}

	// 2. Append to existing section — both bullets must be present.
	if appendErr := mgr.AppendLearning("Prefer atomic.Value"); appendErr != nil {
		t.Fatalf("Second AppendLearning failed: %v", appendErr)
	}

	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file after second append: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "- Use Go 1.26 features") || !strings.Contains(content, "- Prefer atomic.Value") {
		t.Errorf("One of the bullets is missing. Content:\n%s", content)
	}
}

func TestAppendLearning_ExistingSection(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "FEINO.md")
	initial := "# Project\n\n## Agent Learnings\n- Old bullet\n\n## Other Section\nContent\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := NewFileSystemContextManager(tmpDir)
	mgr.activeFile = "FEINO.md"

	if err := mgr.AppendLearning("New insight"); err != nil {
		t.Fatalf("AppendLearning into existing section: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(got)
	if !strings.Contains(content, "- Old bullet") {
		t.Error("existing bullet was lost")
	}
	if !strings.Contains(content, "- New insight") {
		t.Error("new bullet was not inserted")
	}
	// New bullet must be inserted before ## Other Section.
	newIdx := strings.Index(content, "- New insight")
	otherIdx := strings.Index(content, "## Other Section")
	if otherIdx != -1 && newIdx > otherIdx {
		t.Error("new bullet was appended after the next section heading")
	}
}

func TestGetSystemPrompt(t *testing.T) {
	tmpDir := t.TempDir()

	// Error path: no active file.
	mgr := NewFileSystemContextManager(tmpDir)
	if _, err := mgr.GetSystemPrompt(); err == nil {
		t.Fatal("expected error when no active file is set")
	}

	// Happy path: active file present.
	path := filepath.Join(tmpDir, "FEINO.md")
	wantContent := "# My project instructions\n"
	if err := os.WriteFile(path, []byte(wantContent), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr.activeFile = "FEINO.md"

	got, err := mgr.GetSystemPrompt()
	if err != nil {
		t.Fatalf("GetSystemPrompt: %v", err)
	}
	if got != wantContent {
		t.Errorf("got %q, want %q", got, wantContent)
	}
}

func TestAppendLearning_NoActiveFile(t *testing.T) {
	mgr := NewFileSystemContextManager(t.TempDir())
	if err := mgr.AppendLearning("anything"); err == nil {
		t.Fatal("expected error when no active file is set")
	}
}
