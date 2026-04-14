package chat

import (
	"strings"
	"testing"

	"github.com/odinnordico/feino/internal/agent"
	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/tokens"
	"github.com/odinnordico/feino/internal/tui/theme"
)

func TestStatusBar_RenderInputRowWithPercentage(t *testing.T) {
	th := theme.DarkTheme()
	width := 80
	inputView := "hello world"
	tokenPct := 85

	result := renderInputRow(width, "", inputView, tokenPct, th)

	// Should contain prompt
	if !strings.Contains(result, ">>") {
		t.Error("input row should contain the '>>' prompt")
	}

	// Should contain the typed string
	if !strings.Contains(result, "hello world") {
		t.Error("input row should contain the inputView content")
	}

	// Should contain the percentage correctly formatted
	if !strings.Contains(result, "85%") {
		t.Error("input row should contain the token percentage")
	}
}

func TestStatusBar_RenderInputRowWithoutPercentage(t *testing.T) {
	th := theme.DarkTheme()
	width := 80
	inputView := "hello world"

	// negative percentage signals unknown budget and should hide the widget
	tokenPct := -1

	result := renderInputRow(width, "", inputView, tokenPct, th)

	if !strings.Contains(result, "hello world") {
		t.Error("input row should contain the inputView content")
	}

	if strings.Contains(result, "%") || strings.Contains(result, "-1") {
		t.Error("input row should NOT contain a percentage indicator when tokenPct < 0")
	}
}

func TestStatusBar_RenderStatusBar(t *testing.T) {
	th := theme.DarkTheme()
	data := StatusBarData{
		State: agent.StateVerify,
		Usage: tokens.UsageMetadata{
			Usage: model.Usage{
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
			},
		},
		LatencyMs:      1234.6,
		PromptTurn:     20,
		CompletionTurn: 10,
	}

	result := renderStatusBar(100, data, th)

	if !strings.Contains(result, "verifying") { // React state mapping
		t.Errorf("status bar should contain state 'verifying', got %q", result)
	}

	if !strings.Contains(result, "1s") { // Values > 1000ms are shown as seconds
		t.Errorf("status bar should contain formatted latency '1s', got %q", result)
	}

	if !strings.Contains(result, "Turn: 20 prompt / 10 completion") {
		t.Errorf("status bar should contain turn stats, got %q", result)
	}

	if !strings.Contains(result, "Tokens: 100 prompt / 50 completion = 150 total") {
		t.Errorf("status bar should contain token stats, got %q", result)
	}
}

func TestStatusBar_RenderStatusBarNoLatency(t *testing.T) {
	th := theme.DarkTheme()
	data := StatusBarData{
		State: agent.StateInit,
	}

	result := renderStatusBar(100, data, th)

	if !strings.Contains(result, "idle") {
		t.Errorf("status bar should map init state to 'idle'")
	}

	if !strings.Contains(result, "Latency: —") {
		t.Errorf("status bar should show '—' for 0 latency")
	}
}
