package tokens

import (
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/odinnordico/feino/internal/model"
)

// UsageMetadata extends the basic model.Usage to track cumulative costs or estimations
type UsageMetadata struct {
	model.Usage
	EstimatedPromptTokens int
	Timestamp             time.Time
}

// UsageListener defines a callback for when usage changes.
// Listeners are called in a dedicated goroutine and must not call back into the
// UsageManager that invoked them (doing so would deadlock).
type UsageListener func(meta UsageMetadata)

// UsageManager acts as a centralized store or accumulator for tracked API usages.
type UsageManager struct {
	logger       *slog.Logger
	mu           sync.RWMutex
	listeners    []UsageListener
	totalContext UsageMetadata
}

// NewUsageManager initializes a new UsageManager instance.
func NewUsageManager(logger *slog.Logger) *UsageManager {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &UsageManager{logger: logger}
}

// Subscribe adds a listener for real-time usage metadata pulses (e.g. for a UI footer).
func (m *UsageManager) Subscribe(listener UsageListener) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners = append(m.listeners, listener)
}

// RecordEstimation allows the orchestrator to ping the manager with pre-flight inputs.
func (m *UsageManager) RecordEstimation(estimatedPromptTokens int) {
	m.mu.Lock()
	m.totalContext.EstimatedPromptTokens += estimatedPromptTokens
	m.totalContext.Timestamp = time.Now()
	snap := m.totalContext
	listeners := m.listeners
	m.mu.Unlock()

	m.notifyListeners(snap, listeners)
}

// RecordActual merges the returned model.Usage from a successful LLM inference.
// TotalTokens is recomputed from PromptTokens + CompletionTokens so that provider
// inconsistencies (e.g. Anthropic cache tokens) do not corrupt the running total.
func (m *UsageManager) RecordActual(usage model.Usage) {
	m.mu.Lock()
	m.totalContext.PromptTokens += usage.PromptTokens
	m.totalContext.CompletionTokens += usage.CompletionTokens
	m.totalContext.TotalTokens = m.totalContext.PromptTokens + m.totalContext.CompletionTokens
	m.totalContext.Duration += usage.Duration
	m.totalContext.Timestamp = time.Now()
	snap := m.totalContext
	listeners := m.listeners
	m.mu.Unlock()

	m.notifyListeners(snap, listeners)
}

// GetTotal returns the current aggregated state.
func (m *UsageManager) GetTotal() UsageMetadata {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.totalContext
}

// notifyListeners broadcasts snap to each listener in its own goroutine.
// Must be called after releasing m.mu to avoid deadlocks in listener callbacks.
func (m *UsageManager) notifyListeners(snap UsageMetadata, listeners []UsageListener) { //nolint:gocritic // snap is intentionally a copy so each listener gets a stable snapshot, not a shared pointer
	for _, l := range listeners {
		go func(fn UsageListener) {
			defer func() {
				if r := recover(); r != nil {
					m.logger.Error("usage listener panic recovered", "panic", r)
				}
			}()
			fn(snap)
		}(l)
	}
}
