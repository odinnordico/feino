package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/provider"
	"github.com/odinnordico/feino/internal/tokens"
)

const (
	// ZScoreOutlierThreshold defines when a model is suffering severe performance degradation (default Z-Score > 2.0).
	ZScoreOutlierThreshold = 2.0

	// DiscoveryTimeout defines the maximum time the router waits for all model listings in parallel.
	DiscoveryTimeout = 3 * time.Second

	// DefaultMaxRecommendations defines the default number of candidates returned for fallbacks.
	DefaultMaxRecommendations = 6

	// DefaultHighComplexityThreshold defines the default high reasoning barrier.
	DefaultHighComplexityThreshold = 2000

	// DefaultLowComplexityThreshold defines the default speed priority barrier.
	DefaultLowComplexityThreshold = 500

	// MetricRetentionTTL defines how long we keep a model metric if not refreshed (30 days).
	MetricRetentionTTL = 30 * 24 * time.Hour

	// TokenFloor prevents extreme ms/token spikes for near-instant responses.
	TokenFloor = 5
)

type (
	// RouteRecommendation encapsulates a scored model selection for fallbacks.
	RouteRecommendation struct {
		Provider provider.Provider
		Model    model.Model
		Score    float64
	}

	// TACOSRouter implements Token-Adjusted Latency Outliers selection.
	TACOSRouter struct {
		mu                      sync.RWMutex
		logger                  *slog.Logger
		providers               []provider.Provider
		metrics                 map[string]*ModelMetrics // Key: providerID + "::" + modelName
		providerAlphas          map[string]float64       // Key: providerID; EMA alpha per provider
		estimator               tokens.Estimator
		tier1Models             map[string]bool
		modelCache              map[string][]model.Model
		MaxRecommendations      int
		LowComplexityThreshold  int
		HighComplexityThreshold int
		persistencePath         string
		usageCount              int
	}
)

// TACOSOption configures a TACOSRouter.
type TACOSOption func(*TACOSRouter)

// WithPersistencePath overrides the default metrics persistence path (~/.feino/metrics.json).
// Use this in tests to prevent loading or writing production metrics.
func WithPersistencePath(path string) TACOSOption {
	return func(r *TACOSRouter) {
		r.persistencePath = path
	}
}

// WithTACOSLogger sets the logger used by the router.
func WithTACOSLogger(logger *slog.Logger) TACOSOption {
	return func(r *TACOSRouter) {
		r.logger = logger
	}
}

// NewTACOSRouter configures the routing engine.
func NewTACOSRouter(estimator tokens.Estimator, opts ...TACOSOption) *TACOSRouter {
	home, _ := os.UserHomeDir()

	r := &TACOSRouter{
		logger:                  slog.Default(),
		providers:               make([]provider.Provider, 0),
		metrics:                 make(map[string]*ModelMetrics),
		providerAlphas:          make(map[string]float64),
		estimator:               estimator,
		tier1Models:             make(map[string]bool),
		modelCache:              make(map[string][]model.Model),
		MaxRecommendations:      DefaultMaxRecommendations,
		LowComplexityThreshold:  DefaultLowComplexityThreshold,
		HighComplexityThreshold: DefaultHighComplexityThreshold,
		persistencePath:         filepath.Join(home, ".feino", "metrics.json"),
	}

	for _, opt := range opts {
		opt(r)
	}

	r.LoadMetrics()
	return r
}

// LoadMetrics restores performance data from disk.
func (r *TACOSRouter) LoadMetrics() {
	if _, err := os.Stat(r.persistencePath); os.IsNotExist(err) {
		return
	}

	data, err := os.ReadFile(r.persistencePath)
	if err != nil {
		r.logger.Warn("failed to read metrics file", "path", r.persistencePath, "error", err)
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var loaded map[string]*ModelMetrics
	if err := json.Unmarshal(data, &loaded); err != nil {
		r.logger.Warn("failed to unmarshal metrics", "error", err)
		return
	}

	now := time.Now()
	for k, m := range loaded {
		if now.Sub(m.LastUsed) < MetricRetentionTTL {
			r.metrics[k] = m
		}
	}
}

// SaveMetrics persists current metrics to disk after garbage collecting stale ones.
func (r *TACOSRouter) SaveMetrics() {
	r.mu.RLock()
	now := time.Now()
	activeMetrics := make(map[string]*ModelMetrics)
	for k, m := range r.metrics {
		// Read LastUsed under the per-metric lock to avoid racing with
		// AddLatencyPerToken, which writes LastUsed under m.mu.
		m.mu.RLock()
		lastUsed := m.LastUsed
		m.mu.RUnlock()
		if now.Sub(lastUsed) < MetricRetentionTTL {
			activeMetrics[k] = m
		}
	}
	r.mu.RUnlock()

	data, err := json.MarshalIndent(activeMetrics, "", "  ")
	if err != nil {
		r.logger.Error("failed to marshal metrics", "error", err)
		return
	}

	dir := filepath.Dir(r.persistencePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.logger.Error("failed to create persistence directory", "dir", dir, "error", err)
		return
	}

	if err := os.WriteFile(r.persistencePath, data, 0o600); err != nil {
		r.logger.Error("failed to save metrics", "path", r.persistencePath, "error", err)
	}
}

// SetComplexityThresholds dynamically configures the reasoning barriers.
func (r *TACOSRouter) SetComplexityThresholds(low, high int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if low >= 0 {
		r.LowComplexityThreshold = low
	}
	if high > low {
		r.HighComplexityThreshold = high
	}
}

// SetTier1Models dynamically configures which models are considered Tier 1.
func (r *TACOSRouter) SetTier1Models(models []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tier1Models = make(map[string]bool)
	for _, m := range models {
		r.tier1Models[strings.ToLower(m)] = true
	}
}

func (r *TACOSRouter) isTier1(modelName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	normal := strings.ToLower(modelName)
	for k := range r.tier1Models {
		if strings.Contains(normal, k) {
			return true
		}
	}
	return false
}

// RegisterProvider injects a provider engine into the router.
// An optional alpha overrides the default EMA smoothing factor (0.3) for this
// provider's latency metrics. Values outside (0, 1] are ignored.
func (r *TACOSRouter) RegisterProvider(p provider.Provider, alpha ...float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = append(r.providers, p)
	a := 0.3
	if len(alpha) > 0 && alpha[0] > 0 && alpha[0] <= 1 {
		a = alpha[0]
	}
	r.providerAlphas[p.GetID()] = a
}

// routeKey normalizes the metric dictionary keys.
func routeKey(prov provider.Provider, m model.Model) string {
	return fmt.Sprintf("%s::%s", prov.GetID(), m.GetName())
}

func (r *TACOSRouter) getMetrics(key string) *ModelMetrics {
	r.mu.RLock()
	m, exists := r.metrics[key]
	r.mu.RUnlock()

	if !exists {
		r.mu.Lock()
		defer r.mu.Unlock()
		if m, exists = r.metrics[key]; exists {
			return m
		}

		// Derive provider ID from the key (format: "providerID::modelName") and
		// look up the alpha registered at provider registration time.
		alpha := 0.3
		if providerID, _, found := strings.Cut(key, "::"); found {
			if a, ok := r.providerAlphas[providerID]; ok {
				alpha = a
			}
		}

		m = NewModelMetrics(50, alpha)
		r.metrics[key] = m
	}
	return m
}

// discoveryResult pairs a provider with its models for internal scoring.
type discoveryResult struct {
	p    provider.Provider
	mods []model.Model
}

// SelectOptimalModel picks a prioritized list of models taking complexity and API performance outliers into account.
func (r *TACOSRouter) SelectOptimalModel(ctx context.Context, history []model.Message) ([]RouteRecommendation, error) {
	r.mu.RLock()
	provs := r.providers
	maxRecs := r.MaxRecommendations
	r.mu.RUnlock()

	if len(provs) == 0 {
		return nil, fmt.Errorf("no providers registered for TACOS routing")
	}

	results := r.discoverModels(ctx, provs)

	estTokens := 0
	if r.estimator != nil {
		// Use gpt-4 as the encoding reference for complexity classification —
		// it maps to cl100k_base which is a reasonable approximation across all
		// providers, and the result is only used for tier bucketing, not billing.
		estTokens, _ = r.estimator.EstimateMessages(history, "gpt-4")
	}

	recs := r.rankModels(results, estTokens, maxRecs)
	if len(recs) > 0 {
		return recs, nil
	}

	return r.probeEmergency(ctx, provs, maxRecs)
}

func (r *TACOSRouter) discoverModels(ctx context.Context, provs []provider.Provider) []discoveryResult {
	results := make([]discoveryResult, len(provs))

	discoveryCtx, cancel := context.WithTimeout(ctx, DiscoveryTimeout)
	defer cancel()

	var wg sync.WaitGroup
	for i, p := range provs {
		wg.Add(1)
		go func(idx int, prov provider.Provider) {
			defer wg.Done()
			cb := prov.GetCircuitBreaker()
			if cb != nil && cb.State() == provider.CircuitOpen {
				r.logger.Debug("tacos: skipping provider with open circuit breaker", "provider", prov.GetID())
				return
			}
			mods, err := prov.GetModels(discoveryCtx)
			if err != nil {
				r.logger.Warn("tacos: failed to list models for provider", "provider", prov.GetID(), "error", err)
			} else {
				r.logger.Debug("tacos: discovered models", "provider", prov.GetID(), "count", len(mods))
			}
			results[idx] = discoveryResult{p: prov, mods: mods}
		}(i, p)
	}
	wg.Wait()

	final := make([]discoveryResult, 0, len(provs))
	r.mu.Lock()
	for _, res := range results {
		if res.p == nil {
			continue
		}
		mods := res.mods
		if len(mods) == 0 {
			mods = r.modelCache[res.p.GetID()]
			if len(mods) > 0 {
				r.logger.Debug("tacos: using cached models for provider", "provider", res.p.GetID(), "count", len(mods))
			}
		} else {
			r.modelCache[res.p.GetID()] = mods
		}
		if len(mods) > 0 {
			final = append(final, discoveryResult{p: res.p, mods: mods})
		}
	}
	r.mu.Unlock()

	return final
}

func (r *TACOSRouter) rankModels(results []discoveryResult, estTokens, maxRecs int) []RouteRecommendation {
	r.mu.RLock()
	lowThresh := r.LowComplexityThreshold
	highThresh := r.HighComplexityThreshold
	r.mu.RUnlock()

	isSpeedTier := estTokens < lowThresh
	isIntelligenceTier := estTokens >= highThresh

	var recs []RouteRecommendation
	for _, res := range results {
		for _, m := range res.mods {
			metrics := r.getMetrics(routeKey(res.p, m))

			cb := res.p.GetCircuitBreaker()
			if cb != nil && cb.State() == provider.CircuitOpen {
				continue
			}

			mean, _ := metrics.Stats()
			score := mean
			if mean == 0 {
				score = 50.0
			}

			if cb != nil && cb.State() == provider.CircuitHalfOpen {
				score += 2000.0
			}

			tier1 := r.isTier1(m.GetName())
			if isSpeedTier {
				if !tier1 {
					score -= 100.0
				}
			} else if isIntelligenceTier {
				if tier1 {
					score -= 1500.0
				} else {
					score += 500.0
				}
			} else if tier1 {
				score -= 300.0
			}

			if metrics.CurrentZScore() > ZScoreOutlierThreshold {
				score += 5000.0
			}

			recs = append(recs, RouteRecommendation{Provider: res.p, Model: m, Score: score})
		}
	}

	if len(recs) == 0 {
		return nil
	}

	sort.Slice(recs, func(i, j int) bool {
		return recs[i].Score < recs[j].Score
	})

	if len(recs) > maxRecs {
		recs = recs[:maxRecs]
	}

	if len(recs) > 0 {
		r.logger.Debug("tacos: ranked recommendations",
			"top_model", recs[0].Model.GetName(),
			"top_provider", recs[0].Provider.GetID(),
			"top_score", recs[0].Score,
			"candidates", len(recs),
			"estimated_tokens", estTokens,
		)
	}

	return recs
}

func (r *TACOSRouter) probeEmergency(ctx context.Context, provs []provider.Provider, maxRecs int) ([]RouteRecommendation, error) {
	r.logger.Warn("tacos: no ranked candidates found, starting emergency probe", "providers", len(provs))

	var recs []RouteRecommendation
	for _, p := range provs {
		cb := p.GetCircuitBreaker()
		if cb != nil && cb.State() == provider.CircuitOpen {
			r.logger.Debug("tacos: emergency probe skipping provider with open circuit breaker", "provider", p.GetID())
			continue
		}

		mods, err := p.GetModels(ctx)
		if err != nil {
			r.logger.Warn("tacos: emergency probe failed to list models", "provider", p.GetID(), "error", err)
		}
		for _, m := range mods {
			if r.isTier1(m.GetName()) {
				recs = append(recs, RouteRecommendation{Provider: p, Model: m, Score: 0})
			}
		}
		if len(recs) == 0 && len(mods) > 0 {
			recs = append(recs, RouteRecommendation{Provider: p, Model: mods[0], Score: 0})
		}

		if len(recs) > 0 {
			if len(recs) > maxRecs {
				recs = recs[:maxRecs]
			}
			r.logger.Info("tacos: emergency probe succeeded", "provider", p.GetID(), "candidates", len(recs))
			return recs, nil
		}
	}
	r.logger.Error("tacos: emergency probe exhausted all providers with no models")
	return nil, fmt.Errorf("TACOS routing failed: no available models found even after fast probing")
}

// RecordUsage updates the internal metrics following an execution.
func (r *TACOSRouter) RecordUsage(prov provider.Provider, m model.Model, usage model.Usage) {
	key := routeKey(prov, m)
	metrics := r.getMetrics(key)

	tokensOut := usage.CompletionTokens
	if tokensOut <= 0 {
		tokensOut = usage.TotalTokens
	}
	if tokensOut < TokenFloor {
		tokensOut = TokenFloor
	}

	metrics.AddLatencyPerToken(usage.Duration, tokensOut)

	// Reset the counter before spawning the save goroutine so that concurrent
	// calls arriving between the spawn and the save do not queue extra saves.
	r.mu.Lock()
	r.usageCount++
	needsSave := r.usageCount >= 10
	if needsSave {
		r.usageCount = 0
	}
	r.mu.Unlock()

	if needsSave {
		r.logger.Debug("tacos: triggering periodic metrics save", "usage_count_reset", true)
		go r.SaveMetrics()
	}
}
