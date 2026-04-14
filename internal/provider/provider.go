package provider

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/odinnordico/feino/internal/model"
)

// ProviderMetrics tracks per-provider performance and reliability.
// All fields are safe for concurrent increment from multiple goroutines.
type ProviderMetrics struct {
	TotalRequests atomic.Int64
	SuccessCount  atomic.Int64
	FailureCount  atomic.Int64
}

// Provider defines the standard interface for all LLM providers.
type Provider interface {
	GetName() string
	GetID() string
	GetDescription() string
	GetHomepage() string
	GetModels(ctx context.Context) ([]model.Model, error)
	SetModel(m model.Model)
	GetSelectedModel() model.Model
	GetLogger() *slog.Logger
	GetCircuitBreaker() *CircuitBreaker
	GetMetrics() *ProviderMetrics
}
