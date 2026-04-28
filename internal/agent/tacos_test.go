package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/provider"
)

// Mocks

type mockEstimator struct {
	fixedTokens int
}

func (m *mockEstimator) EstimateString(text string, modelName string) (int, error) {
	return m.fixedTokens, nil
}
func (m *mockEstimator) EstimateMessage(msg model.Message, modelName string) (int, error) {
	return m.fixedTokens, nil
}
func (m *mockEstimator) EstimateMessages(msgs []model.Message, modelName string) (int, error) {
	return m.fixedTokens, nil
}

type mockModel struct {
	id   string
	name string
}

func (m mockModel) GetID() string           { return m.id }
func (m mockModel) GetName() string         { return m.name }
func (m mockModel) GetDescription() string  { return "" }
func (m mockModel) GetHomepage() string     { return "" }
func (m mockModel) GetLogger() *slog.Logger { return nil }
func (m mockModel) GetContextWindow() int   { return 4096 }
func (m mockModel) GetMaxOutputTokens() int { return 4096 }
func (m mockModel) SupportsTools() bool     { return false }
func (m mockModel) Infer(ctx context.Context, history []model.Message, opts model.InferOptions, onPart func(model.MessagePart)) (model.Message, model.Usage, error) {
	return nil, model.Usage{}, nil
}

type mockProvider struct {
	id      string
	models  []model.Model
	cb      *provider.CircuitBreaker
	metrics provider.Metrics
}

func (p *mockProvider) GetName() string                                      { return p.id }
func (p *mockProvider) GetID() string                                        { return p.id }
func (p *mockProvider) GetDescription() string                               { return "" }
func (p *mockProvider) GetHomepage() string                                  { return "" }
func (p *mockProvider) SetModel(m model.Model)                               {}
func (p *mockProvider) GetSelectedModel() model.Model                        { return nil }
func (p *mockProvider) GetLogger() *slog.Logger                              { return nil }
func (p *mockProvider) GetModels(ctx context.Context) ([]model.Model, error) { return p.models, nil }
func (p *mockProvider) GetCircuitBreaker() *provider.CircuitBreaker          { return p.cb }
func (p *mockProvider) GetMetrics() *provider.Metrics                        { return &p.metrics }

// Tests

func TestTACOSRouter_Persistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.json")

	router := NewTACOSRouter(nil, WithPersistencePath(path))

	prov := &mockProvider{id: "p1"}
	mod := mockModel{name: "m1"}

	router.RecordUsage(prov, mod, model.Usage{Duration: 100 * time.Millisecond, CompletionTokens: 10})
	router.SaveMetrics()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("Metrics file not saved")
	}

	router2 := NewTACOSRouter(nil, WithPersistencePath(path))

	key := routeKey(prov, mod)
	m := router2.metrics[key]
	if m == nil {
		t.Fatalf("Metrics not loaded for %s", key)
	}

	if m.EWMA != 10.0 {
		t.Errorf("Expected loaded EWMA 10.0, got %.2f", m.EWMA)
	}
}

func TestTACOSRouter_GarbageCollection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.json")

	router := NewTACOSRouter(nil, WithPersistencePath(path))

	p1 := &mockProvider{id: "p1"}
	m1 := mockModel{name: "recent-model"}
	router.RecordUsage(p1, m1, model.Usage{Duration: 100 * time.Millisecond, CompletionTokens: 10})

	p2 := &mockProvider{id: "p2"}
	m2 := mockModel{name: "old-model"}
	router.RecordUsage(p2, m2, model.Usage{Duration: 100 * time.Millisecond, CompletionTokens: 10})

	router.mu.Lock()
	router.metrics[routeKey(p2, m2)].LastUsed = time.Now().Add(-40 * 24 * time.Hour)
	router.mu.Unlock()

	router.SaveMetrics()

	router2 := NewTACOSRouter(nil, WithPersistencePath(path))

	if _, exists := router2.metrics[routeKey(p1, m1)]; !exists {
		t.Errorf("Recent metric should still exist")
	}

	if _, exists := router2.metrics[routeKey(p2, m2)]; exists {
		t.Errorf("Stale metric (40 days old) should have been garbage collected")
	}
}

func TestTACOSRouter_MultiTierRouting(t *testing.T) {
	ctx := context.Background()
	est := &mockEstimator{fixedTokens: 0}
	router := NewTACOSRouter(est, WithPersistencePath(filepath.Join(t.TempDir(), "metrics.json")))
	router.SetTier1Models([]string{"big-brain"})

	prov := &mockProvider{
		id: "p1",
		models: []model.Model{
			mockModel{id: "m1", name: "big-brain"},
			mockModel{id: "m2", name: "fast-light"},
		},
	}
	router.RegisterProvider(prov)

	router.RecordUsage(prov, prov.models[0], model.Usage{Duration: 500 * time.Millisecond, CompletionTokens: 10})
	router.RecordUsage(prov, prov.models[1], model.Usage{Duration: 300 * time.Millisecond, CompletionTokens: 10})

	est.fixedTokens = 100
	recs, _ := router.SelectOptimalModel(ctx, nil)
	if recs[0].Model.GetName() != "fast-light" {
		t.Errorf("Speed Tier: expected fast-light, got %s", recs[0].Model.GetName())
	}

	est.fixedTokens = 1000
	recs, _ = router.SelectOptimalModel(ctx, nil)
	if recs[0].Model.GetName() != "big-brain" {
		t.Errorf("Balanced Tier: expected big-brain, got %s", recs[0].Model.GetName())
	}

	est.fixedTokens = 5000
	recs, _ = router.SelectOptimalModel(ctx, nil)
	if recs[0].Model.GetName() != "big-brain" {
		t.Errorf("Intelligence Tier: expected big-brain, got %s", recs[0].Model.GetName())
	}
}

func TestTACOSRouter_OutlierAvoidance(t *testing.T) {
	ctx := context.Background()
	router := NewTACOSRouter(&mockEstimator{fixedTokens: 50}, WithPersistencePath(filepath.Join(t.TempDir(), "metrics.json")))

	prov := &mockProvider{
		id: "gemini",
		models: []model.Model{
			mockModel{id: "m1", name: "gemini-3.1-pro"},
			mockModel{id: "m2", name: "gemini-3.1-flash"},
		},
	}
	router.RegisterProvider(prov)

	for range 50 {
		router.RecordUsage(prov, prov.models[1], model.Usage{Duration: 200 * time.Millisecond, CompletionTokens: 10})
		router.RecordUsage(prov, prov.models[0], model.Usage{Duration: 500 * time.Millisecond, CompletionTokens: 10})
	}

	router.RecordUsage(prov, prov.models[1], model.Usage{Duration: 20000 * time.Millisecond, CompletionTokens: 10})

	recs, _ := router.SelectOptimalModel(ctx, nil)
	if recs[0].Model.GetName() != "gemini-3.1-pro" {
		t.Errorf("Expected pro because flash is an outlier, got %s", recs[0].Model.GetName())
	}
}

func TestTACOSRouter_CircuitBreakerAwareness(t *testing.T) {
	ctx := context.Background()
	p1 := &mockProvider{
		id:     "healthy",
		models: []model.Model{mockModel{name: "slow-model"}},
		cb:     provider.NewCircuitBreaker(5, time.Minute, slog.Default()),
	}

	p2 := &mockProvider{
		id:     "broken",
		models: []model.Model{mockModel{name: "fast-model"}},
		cb:     provider.NewCircuitBreaker(5, time.Minute, slog.Default()),
	}
	for range 6 {
		p2.cb.RecordFailure()
	}

	router := NewTACOSRouter(nil, WithPersistencePath(filepath.Join(t.TempDir(), "metrics.json")))
	router.RegisterProvider(p1)
	router.RegisterProvider(p2)

	router.RecordUsage(p1, p1.models[0], model.Usage{Duration: 1000 * time.Millisecond, CompletionTokens: 10})
	router.RecordUsage(p2, p2.models[0], model.Usage{Duration: 100 * time.Millisecond, CompletionTokens: 10})

	recs, _ := router.SelectOptimalModel(ctx, nil)
	if recs[0].Model.GetName() != "slow-model" {
		t.Errorf("Expected slow-model due to tripped breaker on fast-model, got %s", recs[0].Model.GetName())
	}
}
