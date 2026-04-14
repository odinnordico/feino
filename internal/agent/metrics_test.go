package agent

import (
	"math"
	"testing"
	"time"
)

func TestModelMetrics_EWMA_Reaction(t *testing.T) {
	// Alpha 0.5 for easy math in test
	metrics := NewModelMetrics(10, 0.5)

	// first request: 10ms/token
	metrics.AddLatencyPerToken(100*time.Millisecond, 10)
	mean, _ := metrics.Stats()
	if mean != 10.0 {
		t.Errorf("Expected bootstrap mean of 10, got %.2f", mean)
	}

	// second request: 30ms/token
	// EWMA = (0.5 * 30) + (0.5 * 10) = 20
	metrics.AddLatencyPerToken(300*time.Millisecond, 10)
	mean, _ = metrics.Stats()
	if mean != 20.0 {
		t.Errorf("Expected EWMA mean of 20, got %.2f", mean)
	}

	// third request: 50ms/token
	// EWMA = (0.5 * 50) + (0.5 * 20) = 35
	metrics.AddLatencyPerToken(500*time.Millisecond, 10)
	mean, _ = metrics.Stats()
	if mean != 35.0 {
		t.Errorf("Expected EWMA mean of 35, got %.2f", mean)
	}
}

func TestModelMetrics_ZScore(t *testing.T) {
	// Use lower alpha for stable baseline testing
	metrics := NewModelMetrics(100, 0.1)

	// Build a stable baseline mean around ~20ms/token with slight noise
	for i := range 50 {
		noise := float64(i%3 - 1) // -1, 0, 1
		metrics.AddLatencyPerToken(time.Duration(200+int64(noise*10))*time.Millisecond, 10)
	}

	mean, _ := metrics.Stats()
	if math.Abs(mean-20.0) > 0.1 {
		t.Errorf("Expected mean around 20, got %.2f", mean)
	}

	// Even if stddev is very small, a 200ms/token request should be an outlier
	if !metrics.IsOutlier(200.0, 2.0) {
		t.Errorf("Expected 200ms/token to be classified as an outlier")
	}

	// Register an outlier: 200ms/token
	metrics.AddLatencyPerToken(2000*time.Millisecond, 10)

	// With alpha=0.1, the EWMA moves slowly:
	// New EWMA = (0.1 * 200) + (0.9 * 20) = 20 + 18 = 38
	mean, _ = metrics.Stats()
	if math.Abs(mean-38.0) > 0.1 {
		t.Errorf("Expected slow-reacting EWMA around 38, got %.2f", mean)
	}
}
