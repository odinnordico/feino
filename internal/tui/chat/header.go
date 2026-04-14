package chat

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/odinnordico/feino/internal/tui/theme"
)

// renderHeader renders a small pill tab at the top-left showing the app name
// and current model. It does not span the full width.
func renderHeader(modelName string, th theme.Theme) string {
	appPart := th.HeaderStyle.Render(" FEINO ")
	modelPart := lipgloss.NewStyle().
		Background(th.Surface).
		Foreground(th.TextDim).
		Padding(0, 1).
		Render(modelName)
	return lipgloss.JoinHorizontal(lipgloss.Center, appPart, modelPart)
}
