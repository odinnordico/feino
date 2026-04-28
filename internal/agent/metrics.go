package agent

import (
	"encoding/json"
	"math"
	"sync"
	"time"
)

// ModelMetrics tracks trailing token-adjusted latencies for TACOS calculations.
type ModelMetrics struct {
	mu sync.RWMutex

	// Ring buffer for O(1) slot eviction.
	buf   []float64
	head  int // next write position
	count int // number of valid entries (0..Capacity)

	EWMA     float64   `json:"ewma"`
	Alpha    float64   `json:"alpha"`
	Capacity int       `json:"capacity"`
	LastUsed time.Time `json:"last_used"`
}

// modelMetricsJSON is the on-disk representation with latencies linearized from
// the ring buffer into insertion order.
type modelMetricsJSON struct {
	Latencies []float64 `json:"latencies"`
	EWMA      float64   `json:"ewma"`
	Alpha     float64   `json:"alpha"`
	Capacity  int       `json:"capacity"`
	LastUsed  time.Time `json:"last_used"`
}

func (m *ModelMetrics) MarshalJSON() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return json.Marshal(modelMetricsJSON{
		Latencies: m.linearize(),
		EWMA:      m.EWMA,
		Alpha:     m.Alpha,
		Capacity:  m.Capacity,
		LastUsed:  m.LastUsed,
	})
}

func (m *ModelMetrics) UnmarshalJSON(data []byte) error {
	var jm modelMetricsJSON
	if err := json.Unmarshal(data, &jm); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.EWMA = jm.EWMA
	m.Alpha = jm.Alpha
	m.Capacity = jm.Capacity
	m.LastUsed = jm.LastUsed

	if m.Capacity <= 0 {
		m.Capacity = 100
	}
	m.buf = make([]float64, m.Capacity)
	lats := jm.Latencies
	if len(lats) > m.Capacity {
		lats = lats[len(lats)-m.Capacity:]
	}
	m.count = len(lats)
	copy(m.buf, lats)
	m.head = m.count % m.Capacity
	return nil
}

// linearize returns the ring buffer contents in insertion order.
// Caller must hold at least RLock.
func (m *ModelMetrics) linearize() []float64 {
	if m.count == 0 {
		return nil
	}
	out := make([]float64, m.count)
	start := (m.head - m.count + m.Capacity) % m.Capacity
	for i := range m.count {
		out[i] = m.buf[(start+i)%m.Capacity]
	}
	return out
}

// NewModelMetrics creates a concurrency-safe window for tracking latencies.
func NewModelMetrics(capacity int, alpha float64) *ModelMetrics {
	if capacity <= 0 {
		capacity = 100
	}
	if alpha <= 0 || alpha > 1 {
		alpha = 0.3
	}
	return &ModelMetrics{
		buf:      make([]float64, capacity),
		Alpha:    alpha,
		Capacity: capacity,
		LastUsed: time.Now(),
	}
}

// AddLatencyPerToken calculates ms/token, updates the EWMA, and writes to the
// ring buffer in O(1).
func (m *ModelMetrics) AddLatencyPerToken(duration time.Duration, tokens int) {
	if tokens <= 0 {
		return
	}
	lpt := float64(duration.Milliseconds()) / float64(tokens)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.LastUsed = time.Now()

	if m.EWMA == 0 {
		m.EWMA = lpt
	} else {
		m.EWMA = (m.Alpha * lpt) + ((1 - m.Alpha) * m.EWMA)
	}

	m.buf[m.head] = lpt
	m.head = (m.head + 1) % m.Capacity
	if m.count < m.Capacity {
		m.count++
	}
}

// windowStats computes the arithmetic mean and sample standard deviation of the
// ring buffer. Using the arithmetic mean (rather than EWMA) for the variance
// calculation produces an unbiased stddev.
// Caller must hold at least RLock.
func (m *ModelMetrics) windowStats() (arithmeticMean, stddev float64) {
	if m.count == 0 {
		return 0, 0
	}
	if m.count == 1 {
		return m.buf[(m.head-1+m.Capacity)%m.Capacity], 0
	}

	start := (m.head - m.count + m.Capacity) % m.Capacity
	sum := 0.0
	for i := range m.count {
		sum += m.buf[(start+i)%m.Capacity]
	}
	arithmeticMean = sum / float64(m.count)

	varianceSum := 0.0
	for i := range m.count {
		diff := m.buf[(start+i)%m.Capacity] - arithmeticMean
		varianceSum += diff * diff
	}
	stddev = math.Sqrt(varianceSum / float64(m.count-1))
	return
}

// Stats returns the EWMA (used for TACOS scoring) and the sample standard
// deviation of the rolling window (used for outlier detection).
func (m *ModelMetrics) Stats() (mean, stddev float64) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.count == 0 {
		return 0, 0
	}
	_, stddev = m.windowStats()
	return m.EWMA, stddev
}

// IsOutlier checks whether currentLpt exceeds the Z-score threshold relative
// to the window's arithmetic mean and sample standard deviation.
func (m *ModelMetrics) IsOutlier(currentLpt, zScoreThreshold float64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	arithmeticMean, stddev := m.windowStats()
	if stddev == 0 {
		return false
	}
	return (currentLpt-arithmeticMean)/stddev > zScoreThreshold
}

// CurrentZScore computes the Z-score for the most recent request.
// All statistics are gathered under a single lock to prevent TOCTOU races
// between reading the latest entry and computing the window statistics.
func (m *ModelMetrics) CurrentZScore() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.count < 2 {
		return 0.0
	}

	recent := m.buf[(m.head-1+m.Capacity)%m.Capacity]
	arithmeticMean, stddev := m.windowStats()
	if stddev == 0 {
		return 0.0
	}
	return (recent - arithmeticMean) / stddev
}
