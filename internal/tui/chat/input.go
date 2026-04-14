package chat

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/odinnordico/feino/internal/i18n"
	"github.com/odinnordico/feino/internal/tui/theme"
)

// completion is a single autocomplete entry for slash commands or @ paths.
type completion struct {
	text        string // full text to insert
	description string // shown dimly beside the text
}

// maxCompletions caps the autocomplete dropdown to keep the overlay small.
const maxCompletions = 12

// inputHeight is the fixed number of visible rows in the textarea.
// model.go uses this to calculate the available viewport height.
const inputHeight = 5

// slashCommandsList returns the slash-command completions with descriptions
// localised to the active language. Called on every keypress while typing a
// "/" prefix so descriptions stay in sync when the language changes at runtime.
func slashCommandsList() []completion {
	cmds := []completion{
		{"/clear", i18n.T("cmd_clear")},
		{"/config", i18n.T("cmd_config")},
		{"/email-setup", i18n.T("cmd_email_setup")},
		{"/history", i18n.T("cmd_history")},
		{"/lang", i18n.T("cmd_lang")},
		{"/profile", i18n.T("cmd_profile")},
		{"/reload-plugins", i18n.T("cmd_reload_plugins")},
		{"/reset", i18n.T("cmd_reset")},
		{"/setup", i18n.T("cmd_setup")},
		{"/theme", i18n.T("cmd_theme")},
		{"/yolo", i18n.T("cmd_yolo")},
		{"/exit", i18n.T("cmd_exit")},
		{"/quit", i18n.T("cmd_quit")},
	}
	slices.SortFunc(cmds, func(a, b completion) int {
		return strings.Compare(a.text, b.text)
	})
	return cmds
}

type completionKind string

const (
	completeKindNone  completionKind = ""
	completeKindSlash completionKind = "slash"
	completeKindAt    completionKind = "at"
)

// inputModel wraps bubbles/textarea and adds autocomplete overlays for both
// slash commands and @ file/directory paths.
type inputModel struct {
	ta           textarea.Model
	th           theme.Theme
	workingDir   string
	completions  []completion
	selectedIdx  int
	showComplete bool
	completeKind completionKind
	// @ completion state — tracked per word being typed.
	atColStart  int    // byte offset of '@' in the current line prefix
	atColEnd    int    // byte offset of cursor in the current line prefix
	currentWord string // the @-prefixed word currently being typed
}

func newInputModel(th theme.Theme, workingDir string) inputModel {
	ta := textarea.New()
	ta.Placeholder = i18n.T("input_placeholder")
	ta.CharLimit = 0 // unlimited
	ta.ShowLineNumbers = false
	ta.EndOfBufferCharacter = 0
	ta.Prompt = ""
	ta.SetHeight(inputHeight)

	// Remap InsertNewline so Enter can be captured by the parent model for
	// message submission; Ctrl+J inserts a literal newline in the textarea.
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j"))

	applyTextareaTheme(&ta, th)

	_ = ta.Focus() // sets focused state; returned blink cmd is issued in Init

	return inputModel{
		ta:         ta,
		th:         th,
		workingDir: workingDir,
	}
}

func (m inputModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m inputModel) Update(msg tea.Msg) (inputModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up":
			if m.showComplete && m.selectedIdx > 0 {
				m.selectedIdx--
				return m, nil
			}
		case "down":
			if m.showComplete && m.selectedIdx < len(m.completions)-1 {
				m.selectedIdx++
				return m, nil
			}
		case "tab":
			if m.showComplete && len(m.completions) > 0 {
				return m.acceptCompletion(), nil
			}
		case "esc":
			if m.showComplete {
				// Dismiss the dropdown without propagating — the parent model
				// would otherwise cancel the in-flight session on esc.
				m.showComplete = false
				m.completions = nil
				return m, nil
			}
		}
	}

	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	m = m.updateCompletions()
	return m, cmd
}

// updateCompletions rebuilds the completion list based on the current input.
func (m inputModel) updateCompletions() inputModel {
	val := m.ta.Value()

	// Slash-command mode: active when the entire input starts with / and has
	// no spaces or newlines yet (still on the first, uncomplemented token).
	if strings.HasPrefix(val, "/") && !strings.ContainsAny(val, " \n") {
		var matches []completion
		for _, sc := range slashCommandsList() {
			if strings.HasPrefix(sc.text, val) {
				matches = append(matches, sc)
			}
		}
		m.completions = matches
		m.completeKind = completeKindSlash
		m.showComplete = len(matches) > 0
		if m.selectedIdx >= len(matches) {
			m.selectedIdx = 0
		}
		return m
	}

	// @ mode: examine the current line up to the cursor column.
	lineNum := m.ta.Line()
	li := m.ta.LineInfo()
	charOffset := li.CharOffset // rune-accurate cursor offset within the line

	lines := strings.Split(val, "\n")
	if lineNum >= len(lines) {
		return m.clearCompletions()
	}
	lineText := lines[lineNum]

	// CharOffset can exceed rune count at end-of-line; guard it.
	runes := []rune(lineText)
	if charOffset > len(runes) {
		charOffset = len(runes)
	}
	linePrefix := string(runes[:charOffset])

	wordStart := strings.LastIndexByte(linePrefix, ' ') + 1
	currentWord := linePrefix[wordStart:]

	if strings.HasPrefix(currentWord, "@") {
		matches := m.atCompletions(currentWord)
		m.completions = matches
		m.completeKind = completeKindAt
		m.atColStart = wordStart
		m.atColEnd = len(linePrefix)
		m.currentWord = currentWord
		m.showComplete = len(matches) > 0
		if m.selectedIdx >= len(matches) {
			m.selectedIdx = 0
		}
		return m
	}

	return m.clearCompletions()
}

func (m inputModel) clearCompletions() inputModel {
	m.completions = nil
	m.showComplete = false
	m.completeKind = completeKindNone
	return m
}

// atCompletions returns path completions for the given @-prefixed word.
func (m *inputModel) atCompletions(atWord string) []completion {
	pathPart := strings.TrimPrefix(atWord, "@")

	searchDir := m.workingDir
	namePrefix := pathPart
	if i := strings.LastIndex(pathPart, "/"); i >= 0 {
		searchDir = filepath.Join(m.workingDir, pathPart[:i+1])
		namePrefix = pathPart[i+1:]
	}

	entries, err := os.ReadDir(searchDir)
	if err != nil {
		return nil
	}

	var results []completion
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !strings.HasPrefix(name, namePrefix) {
			continue
		}
		absEntry := filepath.Join(searchDir, name)
		rel, err := filepath.Rel(m.workingDir, absEntry)
		if err != nil {
			continue
		}
		display := "@" + rel
		desc := "file"
		if e.IsDir() {
			display += "/"
			desc = "directory"
		}
		results = append(results, completion{text: display, description: desc})
		if len(results) >= maxCompletions {
			break
		}
	}
	return results
}

// acceptCompletion writes the highlighted completion into the text field.
// It is a no-op when no completion is active.
func (m inputModel) acceptCompletion() inputModel {
	if !m.showComplete || len(m.completions) == 0 {
		return m
	}
	selected := m.completions[m.selectedIdx].text
	switch m.completeKind {
	case completeKindSlash:
		// Replace the entire value (always starts with /) with the selection.
		m.ta.SetValue(selected)
		// Set cursor to the end of the command.
		m.ta.SetCursor(len(selected))
	case completeKindAt:
		// Delete the partially-typed @-word backwards then insert the selection.
		// This preserves any text before atColStart and after the cursor.
		for range []rune(m.currentWord) {
			m.ta, _ = m.ta.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		}
		m.ta.InsertString(selected)
	}
	m.showComplete = false
	return m
}

// ShowCompletions returns true if the autocomplete dropdown is visible.
func (m inputModel) ShowCompletions() bool { return m.showComplete }

// Kind returns the current completion kind.
func (m inputModel) Kind() completionKind { return m.completeKind }

// AcceptCompletion is the exported variant used by the parent model on Enter.
func (m inputModel) AcceptCompletion() inputModel { return m.acceptCompletion() }

// Value returns the current raw input text.
func (m inputModel) Value() string { return m.ta.Value() }

// Clear resets the input to empty.
func (m inputModel) Clear() inputModel {
	m.ta.Reset()
	m.showComplete = false
	m.completions = nil
	return m
}

// InsertNewline inserts a literal newline at the cursor position.
// Called by the parent model on Ctrl+J.
func (m inputModel) InsertNewline() inputModel {
	m.ta.InsertRune('\n')
	return m
}

// SetTheme applies a new theme.
func (m inputModel) SetTheme(th theme.Theme) inputModel {
	m.th = th
	applyTextareaTheme(&m.ta, th)
	return m
}

// applyTextareaTheme overwrites the textarea's built-in default styles with
// colours from the active theme palette, eliminating the blue cursor-line
// artefact that appears when terminals map ANSI colour 0 to blue.
func applyTextareaTheme(ta *textarea.Model, th theme.Theme) {
	ta.FocusedStyle = th.TextareaFocused
	ta.BlurredStyle = th.TextareaBlurred
}

// SetWidth sets the textarea display width.
func (m inputModel) SetWidth(w int) inputModel {
	m.ta.SetWidth(w)
	return m
}

// SetWorkingDir updates the base directory used for @ path completions.
func (m inputModel) SetWorkingDir(dir string) inputModel {
	m.workingDir = dir
	return m
}

// SetPlaceholder updates the textarea placeholder text.
func (m inputModel) SetPlaceholder(s string) inputModel {
	m.ta.Placeholder = s
	return m
}

// View renders the autocomplete overlay (if active) above the textarea.
// The caller is responsible for wrapping this in the full input row (prompt, pct).
func (m inputModel) View() string {
	inputContent := m.ta.View()

	if !m.showComplete || len(m.completions) == 0 {
		return inputContent
	}

	lines := make([]string, len(m.completions))
	for i, c := range m.completions {
		row := c.text + "  " + lipgloss.NewStyle().
			Foreground(m.th.TextDim).
			Render(c.description)
		if i == m.selectedIdx {
			lines[i] = m.th.SelectedStyle.Render(row)
		} else {
			lines[i] = m.th.CompletionStyle.Render(row)
		}
	}
	overlay := strings.Join(lines, "\n")
	return overlay + "\n" + inputContent
}
