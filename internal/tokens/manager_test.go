package tokens

import (
	"log/slog"
	"testing"
	"time"

	"github.com/odinnordico/feino/internal/model"
)

func listenerTimeout(t *testing.T) <-chan time.Time {
	t.Helper()
	if deadline, ok := t.Deadline(); ok {
		return time.After(time.Until(deadline) - 100*time.Millisecond)
	}
	return time.After(5 * time.Second)
}

func TestUsageManagerEstimation(t *testing.T) {
	mgr := NewUsageManager(slog.Default())

	done := make(chan struct{}, 1)
	mgr.Subscribe(func(meta UsageMetadata) {
		if meta.EstimatedPromptTokens == 150 {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})

	mgr.RecordEstimation(150)

	select {
	case <-done:
	case <-listenerTimeout(t):
		t.Fatal("timed out waiting for listener callback")
	}

	total := mgr.GetTotal()
	if total.EstimatedPromptTokens != 150 {
		t.Errorf("Expected 150 estimated prompt tokens, got %d", total.EstimatedPromptTokens)
	}
}

func TestUsageManagerActualSub(t *testing.T) {
	mgr := NewUsageManager(slog.Default())

	done := make(chan struct{}, 1)
	mgr.Subscribe(func(meta UsageMetadata) {
		if meta.PromptTokens == 50 && meta.CompletionTokens == 20 && meta.TotalTokens == 70 {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})

	mgr.RecordActual(model.Usage{
		PromptTokens:     50,
		CompletionTokens: 20,
		TotalTokens:      70,
	})

	select {
	case <-done:
	case <-listenerTimeout(t):
		t.Fatal("timed out waiting for listener callback")
	}

	total := mgr.GetTotal()
	if total.TotalTokens != 70 {
		t.Errorf("Expected 70 total tokens, got %d", total.TotalTokens)
	}
}
