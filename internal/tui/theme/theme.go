// Package theme provides colour palettes and pre-composed lipgloss styles for
// every TUI component in feino.  Three built-in themes are available:
//   - neo  — phosphor-green on near-black, inspired by classic CRT terminals
//   - dark — Catppuccin Mocha
//   - light — Catppuccin Latte
package theme

import (
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
)

// Palette holds the raw colour tokens for a single theme variant.
type Palette struct {
	Primary    lipgloss.Color
	Secondary  lipgloss.Color
	Background lipgloss.Color
	Surface    lipgloss.Color
	Overlay    lipgloss.Color
	Muted      lipgloss.Color
	Text       lipgloss.Color
	TextDim    lipgloss.Color
	Accent     lipgloss.Color
	Error      lipgloss.Color
	Success    lipgloss.Color
	Warning    lipgloss.Color
}

// Neo — phosphor-green CRT palette.
// Deep near-black background, electric greens, amber warnings, red errors.
var neoPalette = Palette{
	Primary:    lipgloss.Color("#00FF41"), // classic Matrix green
	Secondary:  lipgloss.Color("#00D97E"), // teal-green
	Background: lipgloss.Color("#0D0D0D"), // near-black
	Surface:    lipgloss.Color("#1A1A1A"), // slightly lifted
	Overlay:    lipgloss.Color("#2A2A2A"),
	Muted:      lipgloss.Color("#1F2F1F"), // dark green-tinted bar bg
	Text:       lipgloss.Color("#CCFFCC"), // soft green text
	TextDim:    lipgloss.Color("#4D994D"), // dimmed green
	Accent:     lipgloss.Color("#39FF14"), // neon green
	Error:      lipgloss.Color("#FF3131"), // neon red
	Success:    lipgloss.Color("#00FF41"),
	Warning:    lipgloss.Color("#FFB300"), // amber
}

// Catppuccin Mocha (dark)
var darkPalette = Palette{
	Primary:    lipgloss.Color("#CBA6F7"), // mauve
	Secondary:  lipgloss.Color("#89B4FA"), // blue
	Background: lipgloss.Color("#1E1E2E"),
	Surface:    lipgloss.Color("#313244"),
	Overlay:    lipgloss.Color("#6C7086"),
	Muted:      lipgloss.Color("#45475A"),
	Text:       lipgloss.Color("#CDD6F4"),
	TextDim:    lipgloss.Color("#A6ADC8"),
	Accent:     lipgloss.Color("#A6E3A1"), // green
	Error:      lipgloss.Color("#F38BA8"), // red
	Success:    lipgloss.Color("#A6E3A1"),
	Warning:    lipgloss.Color("#FAB387"), // peach
}

// Catppuccin Latte (light)
var lightPalette = Palette{
	Primary:    lipgloss.Color("#8839EF"), // mauve
	Secondary:  lipgloss.Color("#1E66F5"), // blue
	Background: lipgloss.Color("#EFF1F5"),
	Surface:    lipgloss.Color("#CCD0DA"),
	Overlay:    lipgloss.Color("#9CA0B0"),
	Muted:      lipgloss.Color("#BCC0CC"),
	Text:       lipgloss.Color("#4C4F69"),
	TextDim:    lipgloss.Color("#6C6F85"),
	Accent:     lipgloss.Color("#40A02B"), // green
	Error:      lipgloss.Color("#D20F39"), // red
	Success:    lipgloss.Color("#40A02B"),
	Warning:    lipgloss.Color("#FE640B"), // peach
}

// Theme holds palette colours and pre-composed lipgloss styles for every
// TUI component. Build one with NewTheme; switch themes by calling
// DarkTheme, LightTheme, or AutoTheme.
type Theme struct {
	Palette
	glamourStyle string // "dark" or "light", set at construction time

	// Component styles.
	HeaderStyle     lipgloss.Style
	StatusBarStyle  lipgloss.Style
	UserBubble      lipgloss.Style
	AssistantBubble lipgloss.Style
	InputStyle      lipgloss.Style
	SidebarStyle    lipgloss.Style
	SpinnerStyle    lipgloss.Style
	HelpStyle       lipgloss.Style
	ErrorStyle      lipgloss.Style
	BorderStyle     lipgloss.Style
	CompletionStyle lipgloss.Style // autocomplete suggestion overlay
	SelectedStyle   lipgloss.Style // selected autocomplete entry

	// Textarea sub-styles — applied directly to textarea.FocusedStyle /
	// textarea.BlurredStyle so the widget honours the active palette.
	TextareaFocused textarea.Style
	TextareaBlurred textarea.Style
}

// NewTheme derives all component styles from p.
// isDark controls which glamour stylesheet is selected.
func NewTheme(p Palette, isDark bool) Theme {
	glamourStyle := "light"
	if isDark {
		glamourStyle = "dark"
	}

	blurred := lipgloss.NewStyle().Background(p.Background).Foreground(p.TextDim)

	return Theme{
		Palette:      p,
		glamourStyle: glamourStyle,

		HeaderStyle: lipgloss.NewStyle().
			Background(p.Surface).
			Foreground(p.Primary).
			Bold(true).
			Padding(0, 1),

		StatusBarStyle: lipgloss.NewStyle().
			Background(p.Muted).
			Foreground(p.TextDim).
			Padding(0, 1),

		UserBubble: lipgloss.NewStyle().
			Foreground(p.Secondary).
			Bold(true),

		AssistantBubble: lipgloss.NewStyle().
			Foreground(p.Primary).
			Bold(true),

		InputStyle: lipgloss.NewStyle().
			Background(p.Surface).
			Foreground(p.Text).
			Padding(0, 1),

		SidebarStyle: lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderLeft(true).
			BorderForeground(p.Muted).
			Padding(0, 1),

		SpinnerStyle: lipgloss.NewStyle().
			Foreground(p.Primary),

		HelpStyle: lipgloss.NewStyle().
			Foreground(p.TextDim),

		ErrorStyle: lipgloss.NewStyle().
			Foreground(p.Error).
			Bold(true),

		BorderStyle: lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(p.Muted),

		CompletionStyle: lipgloss.NewStyle().
			Background(p.Surface).
			Foreground(p.Text).
			Padding(0, 1),

		SelectedStyle: lipgloss.NewStyle().
			Background(p.Primary).
			Foreground(p.Background).
			Padding(0, 1),

		TextareaFocused: textarea.Style{
			Base:        lipgloss.NewStyle().Background(p.Overlay).Foreground(p.Text),
			CursorLine:  lipgloss.NewStyle().Background(p.Overlay).Foreground(p.Text),
			Placeholder: lipgloss.NewStyle().Background(p.Overlay).Foreground(p.TextDim),
			Text:        lipgloss.NewStyle().Background(p.Overlay).Foreground(p.Text),
			EndOfBuffer: lipgloss.NewStyle().Background(p.Overlay).Foreground(p.TextDim),
		},
		TextareaBlurred: textarea.Style{
			Base:        blurred,
			CursorLine:  blurred,
			Placeholder: blurred,
			Text:        blurred,
			EndOfBuffer: blurred,
		},
	}
}

// NeoTheme returns the phosphor-green CRT theme.
func NeoTheme() Theme { return NewTheme(neoPalette, true) }

// DarkTheme returns the Catppuccin Mocha dark variant.
func DarkTheme() Theme { return NewTheme(darkPalette, true) }

// LightTheme returns the Catppuccin Latte light variant.
func LightTheme() Theme { return NewTheme(lightPalette, false) }

// AutoTheme picks dark or light based on the terminal's background colour.
func AutoTheme() Theme {
	if lipgloss.HasDarkBackground() {
		return NewTheme(darkPalette, true)
	}
	return NewTheme(lightPalette, false)
}

// FromConfig maps a UIConfig.Theme string to the correct Theme.
// Unknown or empty values (including first-run) fall back to NeoTheme.
func FromConfig(name string) Theme {
	switch name {
	case "dark":
		return DarkTheme()
	case "light":
		return LightTheme()
	case "auto":
		return AutoTheme()
	default: // "neo" and any unrecognised value
		return NeoTheme()
	}
}

// GlamourStyle returns the glamour stylesheet name matching this theme.
func (t Theme) GlamourStyle() string { return t.glamourStyle }
