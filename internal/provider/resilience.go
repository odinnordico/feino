package provider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"net"
	"strings"
	"sync"
	"time"
)

// DefaultRetryConfig returns the standard retry configuration for all providers
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:   3,
		TotalTimeout: 30 * time.Second,
		InitialDelay: 500 * time.Millisecond,
		MaxDelay:     4 * time.Second,
	}
}

// RetryConfig holds configuration for the retry strategy
type RetryConfig struct {
	MaxRetries   int
	TotalTimeout time.Duration
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

// CircuitBreakerState represents the current state of the circuit breaker
type CircuitBreakerState int

const (
	CircuitClosed   CircuitBreakerState = iota // Normal operation
	CircuitOpen                                // Failing fast, rejecting requests
	CircuitHalfOpen                            // Allowing a single probe request
)

func (s CircuitBreakerState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker implements the circuit breaker pattern to fail fast during sustained outages
type CircuitBreaker struct {
	mu                  sync.RWMutex
	state               CircuitBreakerState
	consecutiveFailures int
	failureThreshold    int
	cooldownDuration    time.Duration
	lastFailureTime     time.Time
	logger              *slog.Logger
}

// NewCircuitBreaker creates a circuit breaker with the given failure threshold and cooldown duration
func NewCircuitBreaker(threshold int, cooldown time.Duration, logger *slog.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		state:            CircuitClosed,
		failureThreshold: threshold,
		cooldownDuration: cooldown,
		logger:           logger,
	}
}

// DefaultCircuitBreaker creates a circuit breaker with standard settings (5 failures, 30s cooldown)
func DefaultCircuitBreaker(logger *slog.Logger) *CircuitBreaker {
	return NewCircuitBreaker(5, 30*time.Second, logger)
}

// AllowRequest checks if a request should be allowed through the circuit breaker.
// Transitions from Open to HalfOpen after the cooldown period.
func (cb *CircuitBreaker) AllowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true

	case CircuitOpen:
		if time.Since(cb.lastFailureTime) >= cb.cooldownDuration {
			cb.state = CircuitHalfOpen
			cb.logger.Info("circuit breaker transitioning to half-open", "cooldown", cb.cooldownDuration)
			return true
		}
		return false

	case CircuitHalfOpen:
		return true

	default:
		return true
	}
}

// RecordSuccess resets the circuit breaker to closed state
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state != CircuitClosed {
		cb.logger.Info("circuit breaker closing after successful request", "previous_state", cb.state)
	}
	cb.state = CircuitClosed
	cb.consecutiveFailures = 0
}

// RecordFailure increments the failure counter and opens the circuit if the threshold is reached
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveFailures++
	cb.lastFailureTime = time.Now()

	if cb.consecutiveFailures >= cb.failureThreshold {
		cb.state = CircuitOpen
		cb.logger.Warn("circuit breaker opened",
			"consecutive_failures", cb.consecutiveFailures,
			"threshold", cb.failureThreshold,
			"cooldown", cb.cooldownDuration,
		)
	}
}

// State returns the current state of the circuit breaker
func (cb *CircuitBreaker) State() CircuitBreakerState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// ErrCircuitOpen is returned when the circuit breaker is open and rejecting requests
var ErrCircuitOpen = errors.New("circuit breaker is open: provider is unavailable")

// Retry executes an operation with retry logic and circuit breaker integration.
// The renewFn is called when the error indicates the client needs renewal (e.g., auth failure).
func Retry[T any](
	ctx context.Context,
	cfg RetryConfig,
	cb *CircuitBreaker,
	metrics *Metrics,
	logger *slog.Logger,
	renewFn func(ctx context.Context) error,
	operation func(ctx context.Context) (T, error),
) (T, error) {
	var zero T

	// Apply total timeout to the entire retry sequence
	ctx, cancel := context.WithTimeout(ctx, cfg.TotalTimeout)
	defer cancel()

	var lastErr error
	for attempt := range cfg.MaxRetries + 1 {
		// Check context before each attempt
		if ctx.Err() != nil {
			if lastErr != nil {
				return zero, fmt.Errorf("retry timeout after %d attempts: %w", attempt, lastErr)
			}
			return zero, ctx.Err()
		}

		// Check circuit breaker
		if !cb.AllowRequest() {
			return zero, ErrCircuitOpen
		}

		result, err := operation(ctx)
		if err == nil {
			cb.RecordSuccess()
			if metrics != nil {
				metrics.TotalRequests.Add(1)
				metrics.SuccessCount.Add(1)
			}
			return result, nil
		}

		if metrics != nil {
			metrics.TotalRequests.Add(1)
			metrics.FailureCount.Add(1)
		}
		lastErr = err

		// Non-retryable errors fail immediately
		if !IsRetryable(err) {
			cb.RecordFailure()
			return zero, err
		}

		// If the client needs renewal, attempt it before the next retry
		if NeedsClientRenewal(err) && renewFn != nil {
			logger.Info("attempting client renewal after auth-related error",
				"attempt", attempt+1, "error", err)
			if renewErr := renewFn(ctx); renewErr != nil {
				logger.Warn("client renewal failed", "error", renewErr)
			}
		}

		// Don't sleep after the last attempt
		if attempt >= cfg.MaxRetries {
			break
		}

		delay := retryBackoff(attempt, cfg.InitialDelay, cfg.MaxDelay)
		logger.Warn("retryable error, backing off",
			"attempt", attempt+1,
			"max_attempts", cfg.MaxRetries+1,
			"delay", delay,
			"error", err,
		)

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, fmt.Errorf("retry timeout after %d attempts: %w", attempt+1, lastErr)
		case <-timer.C:
		}
	}

	cb.RecordFailure()
	return zero, fmt.Errorf("operation failed after %d attempts: %w", cfg.MaxRetries+1, lastErr)
}

// retryBackoff calculates the delay for a retry attempt using exponential backoff with jitter
func retryBackoff(attempt int, initialDelay, maxDelay time.Duration) time.Duration {
	delay := min(time.Duration(float64(initialDelay)*math.Pow(2, float64(attempt))), maxDelay)
	// Apply jitter: subtract up to 25% of the delay
	jitter := time.Duration(rand.Int64N(int64(delay / 4)))
	return delay - jitter
}

// IsRetryable determines if an error is transient and worth retrying
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Network errors are always retryable
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return true
	}

	// DNS errors
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	// Context deadline exceeded at the request level (not the total retry timeout)
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	msg := strings.ToLower(err.Error())

	// Rate limiting
	if strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "429") {
		return true
	}

	// Server errors
	if strings.Contains(msg, "500") ||
		strings.Contains(msg, "502") ||
		strings.Contains(msg, "503") ||
		strings.Contains(msg, "529") ||
		strings.Contains(msg, "internal server error") ||
		strings.Contains(msg, "bad gateway") ||
		strings.Contains(msg, "service unavailable") ||
		strings.Contains(msg, "overloaded") {
		return true
	}

	// Connection errors
	if strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "eof") ||
		strings.Contains(msg, "timeout") {
		return true
	}

	if NeedsClientRenewal(err) {
		return true
	}

	return false
}

// NeedsClientRenewal determines if an error indicates the client itself is invalid
// and needs to be recreated (e.g., expired credentials, rotated API key)
func NeedsClientRenewal(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "authentication") ||
		strings.Contains(msg, "invalid api key") ||
		strings.Contains(msg, "invalid_api_key") ||
		strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "403") ||
		strings.Contains(msg, "401")
}
