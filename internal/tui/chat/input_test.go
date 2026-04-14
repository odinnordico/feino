package chat

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/odinnordico/feino/internal/tui/theme"
)

func newTestInput(workingDir string) inputModel {
	return newInputModel(theme.DarkTheme(), workingDir)
}

func sendKey(m inputModel, key string) inputModel {
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	return updated
}

func sendSpecialKey(m inputModel, key tea.KeyType) inputModel {
	updated, _ := m.Update(tea.KeyMsg{Type: key})
	return updated
}

func typeIntoInput(m inputModel, text string) inputModel {
	for _, ch := range text {
		m = sendKey(m, string(ch))
	}
	return m
}

// ── slash-command completions ────────────────────────────────────────────────

func TestInput_SlashCompletions(t *testing.T) {
	m := newTestInput("")
	m = typeIntoInput(m, "/se")
	if !m.showComplete {
		t.Fatal("expected completion dropdown for /se")
	}
	found := false
	for _, c := range m.completions {
		if c.text == "/setup" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected /setup in completions, got %v", m.completions)
	}
}

func TestInput_SlashNoCompletionAfterSpace(t *testing.T) {
	m := newTestInput("")
	m = typeIntoInput(m, "/setup hello")
	if m.showComplete {
		t.Error("completions should not show after space in slash command")
	}
}

func TestInput_EscDismissesCompletions(t *testing.T) {
	m := newTestInput("")
	m = typeIntoInput(m, "/se")
	if !m.showComplete {
		t.Fatal("expected completion dropdown before esc")
	}
	m = sendSpecialKey(m, tea.KeyEsc)
	if m.showComplete {
		t.Error("expected completion dropdown dismissed after esc")
	}
	// Input text should be unchanged.
	if m.Value() != "/se" {
		t.Errorf("input text changed on esc: got %q", m.Value())
	}
}

func TestInput_TabAcceptsCompletion(t *testing.T) {
	m := newTestInput("")
	m = typeIntoInput(m, "/se")
	if !m.showComplete {
		t.Fatal("expected completion dropdown")
	}
	m = sendSpecialKey(m, tea.KeyTab)
	if m.showComplete {
		t.Error("dropdown should close after Tab")
	}
	// Should have accepted the first (or only) completion.
	val := m.Value()
	if len(val) == 0 || val[0] != '/' {
		t.Errorf("unexpected value after Tab: %q", val)
	}
}

func TestInput_UpDownNavigatesCompletions(t *testing.T) {
	m := newTestInput("")
	m = typeIntoInput(m, "/")
	if !m.showComplete {
		t.Fatal("expected completions for /")
	}
	initial := m.selectedIdx
	m = sendSpecialKey(m, tea.KeyDown)
	if m.selectedIdx != initial+1 {
		t.Errorf("Down should advance selectedIdx: got %d, want %d", m.selectedIdx, initial+1)
	}
	m = sendSpecialKey(m, tea.KeyUp)
	if m.selectedIdx != initial {
		t.Errorf("Up should retreat selectedIdx: got %d, want %d", m.selectedIdx, initial)
	}
}

// ── @ path completions ───────────────────────────────────────────────────────

func TestInput_AtCompletions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "src")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	m := newTestInput(dir)
	m = typeIntoInput(m, "@re")
	if !m.showComplete {
		t.Fatal("expected @-completions for @re")
	}
	found := false
	for _, c := range m.completions {
		if c.text == "@readme.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected @readme.md in completions, got %v", m.completions)
	}
}

func TestInput_AtCompletionsCappedAtMax(t *testing.T) {
	dir := t.TempDir()
	// Create more files than maxCompletions.
	for i := range maxCompletions + 5 {
		name := filepath.Join(dir, "file"+string(rune('a'+i))+".go")
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	m := newTestInput(dir)
	m = typeIntoInput(m, "@file")
	if len(m.completions) > maxCompletions {
		t.Errorf("completions not capped: got %d, want ≤ %d", len(m.completions), maxCompletions)
	}
}

func TestInput_AtHiddenFilesExcluded(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "visible.go"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	m := newTestInput(dir)
	m = typeIntoInput(m, "@")
	for _, c := range m.completions {
		if c.text == "@.env" {
			t.Error("hidden file .env should not appear in completions")
		}
	}
}

// ── AcceptCompletion / Clear ─────────────────────────────────────────────────

func TestInput_AcceptCompletionNoOp(t *testing.T) {
	m := newTestInput("")
	m = typeIntoInput(m, "hello")
	m = m.AcceptCompletion() // no-op: no active completion
	if m.Value() != "hello" {
		t.Errorf("AcceptCompletion without active dropdown should not change value: got %q", m.Value())
	}
}

func TestInput_Clear(t *testing.T) {
	m := newTestInput("")
	m = typeIntoInput(m, "/setup")
	m = m.Clear()
	if m.Value() != "" {
		t.Errorf("Clear should empty input, got %q", m.Value())
	}
	if m.showComplete {
		t.Error("Clear should hide completions")
	}
}
