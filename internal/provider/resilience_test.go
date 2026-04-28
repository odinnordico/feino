package provider

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"testing"
	"time"
)

func TestCircuitBreaker(t *testing.T) {
	logger := slog.Default()
	threshold := 3
	cooldown := 100 * time.Millisecond
	cb := NewCircuitBreaker(threshold, cooldown, logger)

	// Initially closed
	if !cb.AllowRequest() {
		t.Error("expected circuit to be closed initially")
	}

	// Record failures until threshold
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Errorf("expected closed state, got %v", cb.State())
	}

	cb.RecordFailure() // 3rd failure
	if cb.State() != CircuitOpen {
		t.Errorf("expected open state after %d failures, got %v", threshold, cb.State())
	}

	if cb.AllowRequest() {
		t.Error("expected AllowRequest to be false when open")
	}

	// Wait for cooldown
	time.Sleep(cooldown + 10*time.Millisecond)

	if !cb.AllowRequest() {
		t.Error("expected AllowRequest to be true (half-open) after cooldown")
	}
	if cb.State() != CircuitHalfOpen {
		t.Errorf("expected half-open state, got %v", cb.State())
	}

	// Successful request closes the circuit
	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Errorf("expected closed state after success, got %v", cb.State())
	}
	if cb.consecutiveFailures != 0 {
		t.Errorf("expected zero failures, got %d", cb.consecutiveFailures)
	}
}

func TestRetry(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries:   2,
		TotalTimeout: 1 * time.Second,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     50 * time.Millisecond,
	}
	logger := slog.Default()
	cb := DefaultCircuitBreaker(logger)

	t.Run("SuccessFirstAttempt", func(t *testing.T) {
		calls := 0
		res, err := Retry(context.Background(), cfg, cb, nil, logger, nil, func(ctx context.Context) (string, error) {
			calls++
			return "ok", nil
		})
		if err != nil || res != "ok" || calls != 1 {
			t.Errorf("expected (ok, nil) and 1 call, got (%v, %v) and %d calls", res, err, calls)
		}
	})

	t.Run("SuccessAfterRetry", func(t *testing.T) {
		calls := 0
		cb.RecordSuccess() // Reset CB
		res, err := Retry(context.Background(), cfg, cb, nil, logger, nil, func(ctx context.Context) (string, error) {
			calls++
			if calls < 2 {
				return "", errors.New("connection reset")
			}
			return "done", nil
		})
		if err != nil || res != "done" || calls != 2 {
			t.Errorf("expected (done, nil) and 2 calls, got (%v, %v) and %d calls", res, err, calls)
		}
	})

	t.Run("ExhaustAllRetries", func(t *testing.T) {
		calls := 0
		cb.RecordSuccess()
		_, err := Retry(context.Background(), cfg, cb, nil, logger, nil, func(ctx context.Context) (string, error) {
			calls++
			return "", errors.New("service unavailable 503")
		})
		if err == nil || calls != 3 {
			t.Errorf("expected error after 3 calls, got %v and %d calls", err, calls)
		}
	})

	t.Run("NonRetryableError", func(t *testing.T) {
		calls := 0
		cb.RecordSuccess()
		_, err := Retry(context.Background(), cfg, cb, nil, logger, nil, func(ctx context.Context) (string, error) {
			calls++
			return "", errors.New("unsupported model version")
		})
		if err == nil || calls != 1 {
			t.Errorf("expected immediate error, got %v and %d calls", err, calls)
		}
	})

	t.Run("RenewalFunction", func(t *testing.T) {
		renewCalls := 0
		retryCalls := 0
		cb.RecordSuccess()
		renewFn := func(ctx context.Context) error {
			renewCalls++
			return nil
		}
		_, err := Retry(context.Background(), cfg, cb, nil, logger, renewFn, func(ctx context.Context) (string, error) {
			retryCalls++
			if retryCalls == 1 {
				return "", errors.New("auth failure: invalid_api_key")
			}
			return "fixed", nil
		})
		if err != nil || renewCalls != 1 || retryCalls != 2 {
			t.Errorf("expected renewal and success, got err=%v, renewCalls=%d, retryCalls=%d", err, renewCalls, retryCalls)
		}
	})

	t.Run("MetricsTracking", func(t *testing.T) {
		cb.RecordSuccess()
		metrics := &Metrics{}
		_, _ = Retry(context.Background(), cfg, cb, metrics, logger, nil, func(ctx context.Context) (string, error) {
			return "", errors.New("service unavailable 503")
		})
		// After 1 call + 2 retries = 3 TotalRequests, 3 FailureCount
		if metrics.TotalRequests.Load() != 3 || metrics.FailureCount.Load() != 3 {
			t.Errorf("expected 3 total and 3 failed requests, got %d and %d", metrics.TotalRequests.Load(), metrics.FailureCount.Load())
		}

		cb.RecordSuccess()
		_, _ = Retry(context.Background(), cfg, cb, metrics, logger, nil, func(ctx context.Context) (string, error) {
			return "ok", nil
		})
		// 1 more successful call
		if metrics.TotalRequests.Load() != 4 || metrics.SuccessCount.Load() != 1 {
			t.Errorf("expected 4 total and 1 successful request, got %d and %d", metrics.TotalRequests.Load(), metrics.SuccessCount.Load())
		}
	})
}

func TestErrorClassifiers(t *testing.T) {
	t.Run("IsRetryable", func(t *testing.T) {
		cases := []struct {
			err      error
			expected bool
		}{
			{nil, false},
			{errors.New("other error"), false},
			{&net.OpError{Op: "dial"}, true},
			{context.DeadlineExceeded, true},
			{errors.New("rate limit reached"), true},
			{errors.New("HTTP 429 Too Many Requests"), true},
			{errors.New("internal server error 500"), true},
			{errors.New("overloaded"), true},
		}
		for _, tc := range cases {
			if got := IsRetryable(tc.err); got != tc.expected {
				t.Errorf("IsRetryable(%v) = %v, want %v", tc.err, got, tc.expected)
			}
		}
	})

	t.Run("NeedsClientRenewal", func(t *testing.T) {
		cases := []struct {
			err      error
			expected bool
		}{
			{nil, false},
			{errors.New("connection reset"), false},
			{errors.New("401 Unauthorized"), true},
			{errors.New("invalid api key"), true},
			{errors.New("authentication failed"), true},
		}
		for _, tc := range cases {
			if got := NeedsClientRenewal(tc.err); got != tc.expected {
				t.Errorf("NeedsClientRenewal(%v) = %v, want %v", tc.err, got, tc.expected)
			}
		}
	})
}
