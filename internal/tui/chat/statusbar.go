package chat

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/odinnordico/feino/internal/agent"
	"github.com/odinnordico/feino/internal/i18n"
	"github.com/odinnordico/feino/internal/tokens"
	"github.com/odinnordico/feino/internal/tui/theme"
)

// StatusBarData holds the volatile metrics displayed in the status bar.
type StatusBarData struct {
	State          agent.ReActState
	Usage          tokens.UsageMetadata
	LatencyMs      float64
	PromptTurn     int
	CompletionTurn int
	BypassActive   bool // true when yolo/bypass mode is on
}

// reactStateMsgIDs maps ReAct states to i18n message IDs for the status bar.
var reactStateMsgIDs = map[agent.ReActState]string{
	agent.StateInit:     "state_idle",
	agent.StateGather:   "state_gathering",
	agent.StateAct:      "state_thinking",
	agent.StateVerify:   "state_verifying",
	agent.StateComplete: "state_done",
	agent.StateFailed:   "state_error",
}

// renderStatusBar renders the 1-line status bar:
//
//	state   Latency: 123ms   Turn: 12p/34c   Tokens: 12p/34c/46 total
func renderStatusBar(width int, data StatusBarData, th theme.Theme) string {
	stateStr := i18n.T(reactStateMsgIDs[data.State])
	if stateStr == "" {
		stateStr = string(data.State)
	}
	if data.BypassActive {
		stateStr += " -- " + i18n.T("status_unsafe")
	}
	stateColor := th.Text
	if data.BypassActive {
		stateColor = th.Warning
	}
	stateRendered := lipgloss.NewStyle().
		Background(th.Muted).
		Foreground(stateColor).
		Bold(data.BypassActive).
		Padding(0, 1).
		Render(stateStr)

	latencyVal := "—"
	if data.LatencyMs > 0 {
		if data.LatencyMs > 1000 {
			latencyVal = fmt.Sprintf("%.0fs", data.LatencyMs/1000)
		} else {
			latencyVal = fmt.Sprintf("%.0fms", data.LatencyMs)
		}
	}
	latencyRendered := lipgloss.NewStyle().
		Background(th.Muted).
		Foreground(th.Accent).
		Padding(0, 1).
		Render(i18n.Tf("status_latency_format", map[string]any{"Value": latencyVal}))

	turnRendered := lipgloss.NewStyle().
		Background(th.Muted).
		Foreground(th.Secondary).
		Padding(0, 1).
		Render(i18n.Tf("status_turn_format", map[string]any{
			"Prompt":     data.PromptTurn,
			"Completion": data.CompletionTurn,
		}))

	tokenRendered := lipgloss.NewStyle().
		Background(th.Muted).
		Foreground(th.TextDim).
		Padding(0, 1).
		Render(i18n.Tf("status_tokens_format", map[string]any{
			"Prompt":     data.Usage.PromptTokens,
			"Completion": data.Usage.CompletionTokens,
			"Total":      data.Usage.TotalTokens,
		}))

	rightSide := lipgloss.JoinHorizontal(lipgloss.Center, latencyRendered, turnRendered, tokenRendered)
	gapWidth := max(width-lipgloss.Width(stateRendered)-lipgloss.Width(rightSide), 0)
	gap := lipgloss.NewStyle().
		Background(th.Muted).
		Render(strings.Repeat(" ", gapWidth))

	return lipgloss.JoinHorizontal(lipgloss.Center, stateRendered, gap, rightSide)
}

// renderInputRow renders the bottom single-line input area:
//
//	>> [input field]                              100%
func renderInputRow(width int, spinView string, inputView string, tokenPct int, th theme.Theme) string {
	prompt := lipgloss.NewStyle().
		Foreground(th.Primary).
		Bold(true).
		Render(">> ")

	if spinView != "" {
		spinView += " "
	}

	// tokenPct < 0 means the provider did not report a budget — hide the indicator.
	var pctRendered string
	if tokenPct >= 0 {
		pctRendered = th.HeaderStyle.Render(fmt.Sprintf(" %d%% ", tokenPct))
	}

	promptWidth := lipgloss.Width(prompt)
	spinWidth := lipgloss.Width(spinView)
	pctWidth := lipgloss.Width(pctRendered)
	inputWidth := max(width-promptWidth-spinWidth-pctWidth, 1)

	inputRendered := lipgloss.NewStyle().
		Width(inputWidth).
		Render(inputView)

	if pctRendered == "" {
		return lipgloss.JoinHorizontal(lipgloss.Top, prompt, spinView, inputRendered)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, prompt, spinView, inputRendered, pctRendered)
}
