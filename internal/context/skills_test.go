package context

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── parseFrontmatter ──────────────────────────────────────────────────────────

func TestParseFrontmatter_Complete(t *testing.T) {
	input := `---
name: generate-pr
description: Automates PR generation.
parameters:
  - name: branch
    type: string
    required: true
    description: Target branch.
  - name: draft
    type: bool
    required: false
    description: Open as draft.
---

## Instructions

Step 1: do something.
`
	fm, body, err := parseFrontmatter([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm == nil {
		t.Fatal("expected non-nil frontmatter")
	}
	if fm.Name != "generate-pr" {
		t.Errorf("Name: want %q, got %q", "generate-pr", fm.Name)
	}
	if fm.Description != "Automates PR generation." {
		t.Errorf("Description: want %q, got %q", "Automates PR generation.", fm.Description)
	}
	if len(fm.Parameters) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(fm.Parameters))
	}
	if fm.Parameters[0].Name != "branch" || !fm.Parameters[0].Required {
		t.Errorf("first parameter mismatch: %+v", fm.Parameters[0])
	}
	if fm.Parameters[1].Name != "draft" || fm.Parameters[1].Required {
		t.Errorf("second parameter mismatch: %+v", fm.Parameters[1])
	}
	if !strings.Contains(string(body), "Step 1") {
		t.Errorf("expected body to contain 'Step 1', got %q", string(body))
	}
}

func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	input := "# Just a heading\n\nSome body content.\n"
	fm, body, err := parseFrontmatter([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm != nil {
		t.Errorf("expected nil frontmatter for file without ---, got %+v", fm)
	}
	if string(body) != input {
		t.Errorf("expected full file as body, got %q", string(body))
	}
}

func TestParseFrontmatter_UnclosedFence(t *testing.T) {
	input := "---\nname: oops\n# no closing fence\n"
	_, _, err := parseFrontmatter([]byte(input))
	if err == nil {
		t.Fatal("expected error for unclosed frontmatter, got nil")
	}
}

func TestParseFrontmatter_InvalidYAML(t *testing.T) {
	input := "---\nname: [unclosed bracket\n---\nbody\n"
	_, _, err := parseFrontmatter([]byte(input))
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

// ── parseSkillFile ────────────────────────────────────────────────────────────

func TestParseSkillFile_MissingName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-name.md")
	content := "---\ndescription: Missing name field.\n---\n\nBody.\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := parseSkillFile(path)
	if err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
}

func TestParseSkillFile_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skill.md")
	content := "---\nname: my-skill\ndescription: Does things.\n---\n\nDo the thing.\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := parseSkillFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Name != "my-skill" {
		t.Errorf("Name: want %q, got %q", "my-skill", s.Name)
	}
	if s.SourceFile != path {
		t.Errorf("SourceFile: want %q, got %q", path, s.SourceFile)
	}
	if !strings.Contains(s.Body, "Do the thing") {
		t.Errorf("Body missing expected content: %q", s.Body)
	}
}

// ── loadSkillsDir ─────────────────────────────────────────────────────────────

func TestLoadSkillsDir_Mixed(t *testing.T) {
	dir := t.TempDir()

	writeSkill := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	writeSkill("valid1.md", "---\nname: skill-one\ndescription: First.\n---\n\nInstructions.\n")
	writeSkill("valid2.md", "---\nname: skill-two\ndescription: Second.\n---\n\nMore instructions.\n")
	writeSkill("invalid.md", "---\nname: [bad yaml\n---\n\nBody.\n")

	skills, err := loadSkillsDir(dir, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 2 {
		t.Errorf("expected 2 valid skills, got %d", len(skills))
	}
}

func TestLoadSkillsDir_Empty(t *testing.T) {
	dir := t.TempDir()
	skills, err := loadSkillsDir(dir, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills in empty dir, got %d", len(skills))
	}
}

// ── discoverSkillsDir ─────────────────────────────────────────────────────────

func TestDiscoverSkillsDir_Priority(t *testing.T) {
	root := t.TempDir()

	// Create both .feino/skills and .claude/skills
	for _, d := range []string{".feino/skills", ".claude/skills"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0755); err != nil {
			t.Fatal(err)
		}
	}

	got, ok := discoverSkillsDir(root)
	if !ok {
		t.Fatal("expected a directory to be found")
	}
	if got != ".feino/skills" {
		t.Errorf("expected .feino/skills to win priority, got %q", got)
	}
}

func TestDiscoverSkillsDir_FallbackToSecond(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude/skills"), 0755); err != nil {
		t.Fatal(err)
	}
	got, ok := discoverSkillsDir(root)
	if !ok {
		t.Fatal("expected .claude/skills to be found")
	}
	if got != ".claude/skills" {
		t.Errorf("expected .claude/skills, got %q", got)
	}
}

func TestDiscoverSkillsDir_NoneExist(t *testing.T) {
	root := t.TempDir()
	_, ok := discoverSkillsDir(root)
	if ok {
		t.Error("expected no directory to be found in empty root")
	}
}

// ── AssembleContext with skills ───────────────────────────────────────────────

func TestAssembleContext_IncludesSkills(t *testing.T) {
	tmpDir := t.TempDir()

	projectFile := filepath.Join(tmpDir, "FEINO.md")
	if err := os.WriteFile(projectFile, []byte("Project instructions"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := NewFileSystemContextManager(tmpDir)
	mgr.activeFile = "FEINO.md"
	mgr.globalConfigPath = ""

	mgr.skills = []Skill{
		{
			Name:        "deploy-service",
			Description: "Deploys the service to production.",
			Parameters: []SkillParameter{
				{Name: "env", Type: "string", Required: true, Description: "Target environment."},
			},
			Body: "## Steps\nRun deploy script.",
		},
	}

	mgr.codeChunks = []SemanticChunk{
		{Name: "SomeFunc", Content: "func SomeFunc() {}", FilePath: "main.go"},
	}

	prompt, err := mgr.AssembleContext(t.Context(), 50000)
	if err != nil {
		t.Fatalf("AssembleContext: %v", err)
	}

	// Skills section must appear inside <available_skills> tags.
	if !strings.Contains(prompt, "<available_skills>") {
		t.Error("missing '<available_skills>' section")
	}
	if !strings.Contains(prompt, "deploy-service") {
		t.Error("missing skill name in output")
	}
	if !strings.Contains(prompt, "Deploys the service to production") {
		t.Error("missing skill description in output")
	}
	// Template renders: `env` (string, required): Target environment.
	if !strings.Contains(prompt, "`env` (string, required)") {
		t.Error("missing parameter in output")
	}
	if !strings.Contains(prompt, "Run deploy script") {
		t.Error("missing skill body in output")
	}

	// Skills must appear before codebase context.
	skillsIdx := strings.Index(prompt, "<available_skills>")
	codeIdx := strings.Index(prompt, "<codebase_context>")
	if codeIdx != -1 && skillsIdx > codeIdx {
		t.Error("skills section must appear before codebase context")
	}
}
