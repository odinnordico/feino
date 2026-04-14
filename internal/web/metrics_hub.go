package web

import (
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	feinov1 "github.com/odinnordico/feino/gen/feino/v1"
	"github.com/odinnordico/feino/internal/app"
	"github.com/odinnordico/feino/internal/tokens"
)

// metricsHub listens to the global session event stream and republishes
// aggregated MetricsEvent values to any registered stream subscribers.
// Each StreamMetrics stream receives its own buffered channel.
type metricsHub struct {
	mu   sync.Mutex
	subs map[string]chan *feinov1.MetricsEvent

	// stateMu guards currentState — session events fire from multiple goroutines.
	stateMu      sync.Mutex
	currentState string
}

func newMetricsHub(sess *app.Session) *metricsHub {
	h := &metricsHub{
		subs: make(map[string]chan *feinov1.MetricsEvent),
	}

	sess.Subscribe(func(e app.Event) {
		switch e.Kind {
		case app.EventStateChanged:
			h.stateMu.Lock()
			if s, ok := e.Payload.(interface{ String() string }); ok {
				h.currentState = s.String()
			} else {
				h.currentState = "unknown"
			}
			h.stateMu.Unlock()

		case app.EventUsageUpdated:
			meta, ok := e.Payload.(tokens.UsageMetadata)
			if !ok {
				return
			}
			h.stateMu.Lock()
			state := h.currentState
			h.stateMu.Unlock()

			evt := &feinov1.MetricsEvent{
				Usage: &feinov1.UsageMetadata{
					PromptTokens:     int32(meta.PromptTokens),
					CompletionTokens: int32(meta.CompletionTokens),
					TotalTokens:      int32(meta.TotalTokens),
				},
				LatencyMs:  float64(meta.Usage.Duration.Milliseconds()),
				ReactState: state,
				Timestamp:  timestamppb.New(time.Now()),
			}
			h.broadcast(evt)
		}
	})

	return h
}

// Subscribe registers a MetricsEvent channel for the given ID. The returned
// cancel function removes the subscription.
func (h *metricsHub) Subscribe(id string) (<-chan *feinov1.MetricsEvent, func()) {
	ch := make(chan *feinov1.MetricsEvent, 32)
	h.mu.Lock()
	h.subs[id] = ch
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		delete(h.subs, id)
		h.mu.Unlock()
		// Do not close ch to avoid send-on-closed-channel races.
	}
	return ch, cancel
}

func (h *metricsHub) broadcast(evt *feinov1.MetricsEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subs {
		select {
		case ch <- evt:
		default: // drop rather than block
		}
	}
}
