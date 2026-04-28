package chat

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/odinnordico/feino/internal/config"
	"github.com/odinnordico/feino/internal/i18n"
	"github.com/odinnordico/feino/internal/tui/theme"
)

// pickerNav handles left/right/tab/enter/esc for all inline picker overlays.
// Returns the updated index and two booleans indicating Enter or Esc.
func pickerNav(key string, idx, count int) (newIdx int, enter, esc bool) {
	switch key {
	case "left":
		if idx > 0 {
			idx--
		}
	case "right", "tab":
		if idx < count-1 {
			idx++
		}
	case "enter":
		return idx, true, false
	case "esc":
		return idx, false, true
	}
	return idx, false, false
}

// ── /yolo picker ─────────────────────────────────────────────────────────────

// yoloDuration is one selectable option in the /yolo duration picker.
type yoloDuration struct {
	msgID    string        // i18n message ID for the button label
	duration time.Duration // zero = session-long (never expires automatically)
}

// yoloDurations are the options offered by the /yolo picker.
// Zero duration means "active for the whole session".
var yoloDurations = []yoloDuration{
	{"yolo_5min", 5 * time.Minute},
	{"yolo_10min", 10 * time.Minute},
	{"yolo_30min", 30 * time.Minute},
	{"yolo_session", 0},
}

// yoloPicker tracks the state of the /yolo duration-selection widget.
type yoloPicker struct {
	selectedIdx int
}

// activateYolo enables bypass mode for the duration selected in the picker and
// arms a tea.Tick to restore safe mode when the window expires.
func (m Model) activateYolo(idx int) (tea.Model, tea.Cmd) {
	m.yoloPick = nil
	chosen := yoloDurations[idx]

	if chosen.duration == 0 {
		m.sess.SetBypassMode(time.Time{})
		m = m.appendInfoText(i18n.T("yolo_active_session"))
		return m, nil
	}

	until := time.Now().Add(chosen.duration)
	m.sess.SetBypassMode(until)
	m = m.appendInfoText(i18n.Tf("yolo_active_timed", map[string]any{
		"Duration": i18n.T(chosen.msgID),
		"Until":    until.Format("15:04:05"),
	}))

	return m, tea.Tick(chosen.duration, func(time.Time) tea.Msg {
		return YoloExpiredMsg{}
	})
}

// renderYoloPickerRow renders the /yolo duration-selection widget that replaces
// the normal input row while the picker is active.
//
// Layout (inputHeight rows):
//
//	⚡ UNSAFE MODE — All tool calls will be auto-approved.
//	   Select how long bypass mode should stay active:
//
//	   ← → or Tab to select · Enter to confirm · Esc to cancel
//
//	   [ 5 min ]   [ 10 min ]   [ 30 min ]   [● Session ]
func (m Model) renderYoloPickerRow() string {
	titleStyle := lipgloss.NewStyle().Foreground(m.th.Warning).Bold(true)
	infoStyle := lipgloss.NewStyle().Foreground(m.th.TextDim)
	hintStyle := lipgloss.NewStyle().Foreground(m.th.TextDim)

	title := titleStyle.Render("  " + i18n.T("yolo_title"))
	info := infoStyle.Render("     " + i18n.T("yolo_subtitle"))
	hint := hintStyle.Render("     " + i18n.T("yolo_hint"))

	var buttons []string
	for i, opt := range yoloDurations {
		label := "  " + i18n.T(opt.msgID) + "  "
		if i == m.yoloPick.selectedIdx {
			buttons = append(buttons, m.th.SelectedStyle.Render(label))
		} else {
			buttons = append(buttons, m.th.CompletionStyle.Render(label))
		}
	}
	btnRow := "     " + strings.Join(buttons, "  ")

	lines := []string{title, info, "", hint, btnRow}
	return lipgloss.NewStyle().Width(m.width).Render(strings.Join(lines, "\n"))
}

// ── /lang picker ──────────────────────────────────────────────────────────────

// langEntry is one selectable language in the /lang picker.
type langEntry struct {
	code    string // BCP 47 tag passed to i18n.Init
	display string // native-script name, always shown regardless of active language
}

// langEntries lists every language feino ships with.
// Display names use native script so every language is recognisable from any
// active locale (e.g. a Russian speaker can still find "Русский").
var langEntries = []langEntry{
	{"en", "English"},
	{"es-419", "Español (Latinoamérica)"},
	{"es-ES", "Español (España)"},
	{"pt-BR", "Português (Brasil)"},
	{"pt-PT", "Português (Europa)"},
	{"zh-Hans", "中文 (简体)"},
	{"ja", "日本語"},
	{"ru", "Русский"},
}

// langPicker tracks the state of the /lang language-selection widget.
type langPicker struct {
	selectedIdx int
}

// activateLang switches the UI language to the selection made in the picker,
// persists the change to config on disk, and dismisses the picker.
func (m Model) activateLang(idx int) (tea.Model, tea.Cmd) {
	m.langPick = nil
	chosen := langEntries[idx]

	i18n.Init(chosen.code)
	m.cfg.UI.Language = chosen.code
	m.input = m.input.SetPlaceholder(i18n.T("input_placeholder"))

	if cfgPath, err := config.DefaultConfigPath(); err == nil {
		_ = config.Save(cfgPath, m.cfg)
	}

	m = m.appendInfoText(i18n.Tf("lang_changed", map[string]any{"Lang": chosen.display}))
	return m, nil
}

// renderLangPickerRow renders the /lang language-selection widget.
//
// Layout (inputHeight rows):
//
//	🌐 Select UI language:
//
//	   ← → or Tab to select · Enter to confirm · Esc to cancel
//
//	   [ English ]   [ Español (Latinoamérica) ]   …
func (m Model) renderLangPickerRow() string {
	titleStyle := lipgloss.NewStyle().Foreground(m.th.Primary).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(m.th.TextDim)

	title := titleStyle.Render("  " + i18n.T("lang_title"))
	hint := hintStyle.Render("     " + i18n.T("lang_hint"))

	var buttons []string
	for i, e := range langEntries {
		label := "  " + e.display + "  "
		if i == m.langPick.selectedIdx {
			buttons = append(buttons, m.th.SelectedStyle.Render(label))
		} else {
			buttons = append(buttons, m.th.CompletionStyle.Render(label))
		}
	}
	btnRow := "     " + strings.Join(buttons, "  ")

	lines := []string{title, "", hint, "", btnRow}
	return lipgloss.NewStyle().Width(m.width).Render(strings.Join(lines, "\n"))
}

// ── /theme picker ─────────────────────────────────────────────────────────────

// themeEntry is one selectable option in the /theme picker.
type themeEntry struct {
	code  string // value stored in config (e.g. "neo")
	msgID string // i18n key for the display label
}

var themeEntries = []themeEntry{
	{"neo", "wizard_theme_neo"},
	{"dark", "wizard_theme_dark"},
	{"light", "wizard_theme_light"},
	{"auto", "wizard_theme_auto"},
}

// themePicker tracks the state of the /theme selection widget.
type themePicker struct {
	selectedIdx int
}

// activateTheme applies the selected theme, persists it, and dismisses the picker.
func (m Model) activateTheme(idx int) (tea.Model, tea.Cmd) {
	m.themePick = nil
	chosen := themeEntries[idx]
	m.cfg.UI.Theme = chosen.code
	m.th = theme.FromConfig(chosen.code)
	m.input = m.input.SetTheme(m.th)
	m.spin.Style = m.th.SpinnerStyle

	if cfgPath, err := config.DefaultConfigPath(); err == nil {
		_ = config.Save(cfgPath, m.cfg)
	}

	m = m.appendInfoText(i18n.Tf("theme_changed", map[string]any{"Theme": i18n.T(chosen.msgID)}))
	m = m.handleResize()
	return m, nil
}

// renderThemePickerRow renders the /theme selection widget.
func (m Model) renderThemePickerRow() string {
	titleStyle := lipgloss.NewStyle().Foreground(m.th.Primary).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(m.th.TextDim)

	title := titleStyle.Render("  " + i18n.T("theme_title"))
	hint := hintStyle.Render("     " + i18n.T("theme_hint"))

	var buttons []string
	for i, e := range themeEntries {
		label := "  " + i18n.T(e.msgID) + "  "
		if i == m.themePick.selectedIdx {
			buttons = append(buttons, m.th.SelectedStyle.Render(label))
		} else {
			buttons = append(buttons, m.th.CompletionStyle.Render(label))
		}
	}
	btnRow := "     " + strings.Join(buttons, "  ")

	lines := []string{title, "", hint, "", btnRow}
	return lipgloss.NewStyle().Width(m.width).Render(strings.Join(lines, "\n"))
}

// ── permission prompt ─────────────────────────────────────────────────────────

// permissionPrompt tracks a pending security-gate approval request.
// The session's ReAct goroutine blocks on response until the user answers.
// choice is false (Deny) by default so the safer option is pre-selected.
type permissionPrompt struct {
	toolName string
	required string
	allowed  string
	response chan<- bool
	choice   bool // true = Allow highlighted; false = Deny highlighted (default)
}

// resolvePermission sends the user's choice to the waiting ReAct goroutine and
// clears the permission prompt, restoring the normal input row.
func (m Model) resolvePermission(approved bool) (tea.Model, tea.Cmd) {
	p := m.permPrompt
	p.response <- approved
	m.permPrompt = nil
	if approved {
		m = m.appendInfoText(i18n.Tf("perm_approved", map[string]any{"Tool": p.toolName}))
	} else {
		m = m.appendInfoText(i18n.Tf("perm_denied", map[string]any{"Tool": p.toolName}))
	}
	return m, nil
}

// renderPermissionRow renders the permission-approval widget that replaces the
// normal input row while a permission prompt is pending.
//
// Layout (inputHeight rows):
//
//	⚠  Allow "file_write" to use write access?
//	   Current permission mode: read
//
//	   ← → or Tab to select · Enter to confirm · Esc to deny
//
//	      [ Allow ]         [● Deny  ]
func (m Model) renderPermissionRow() string {
	p := m.permPrompt

	titleStyle := lipgloss.NewStyle().Foreground(m.th.Warning).Bold(true)
	infoStyle := lipgloss.NewStyle().Foreground(m.th.TextDim)
	hintStyle := lipgloss.NewStyle().Foreground(m.th.TextDim)

	title := titleStyle.Render("  " + i18n.Tf("perm_title", map[string]any{"Tool": p.toolName, "Required": p.required}))
	info := infoStyle.Render("     " + i18n.Tf("perm_current_mode", map[string]any{"Allowed": p.allowed}))
	hint := hintStyle.Render("     " + i18n.T("perm_hint"))

	allowLabel := "  " + i18n.T("perm_allow") + "  "
	denyLabel := "  " + i18n.T("perm_deny") + "   "
	var allowBtn, denyBtn string
	if p.choice {
		allowBtn = m.th.SelectedStyle.Render(allowLabel)
		denyBtn = m.th.CompletionStyle.Render(denyLabel)
	} else {
		allowBtn = m.th.CompletionStyle.Render(allowLabel)
		denyBtn = m.th.SelectedStyle.Render(denyLabel)
	}
	buttons := "     " + allowBtn + "    " + denyBtn

	lines := []string{title, info, "", hint, buttons}
	return lipgloss.NewStyle().Width(m.width).Render(strings.Join(lines, "\n"))
}
