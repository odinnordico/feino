package tokens

import (
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/pkoukk/tiktoken-go"
	"golang.org/x/sync/singleflight"

	"github.com/odinnordico/feino/internal/model"
)

// Estimator defines the interface for token estimation
type Estimator interface {
	EstimateString(text string, modelName string) (int, error)
	EstimateMessage(msg model.Message, modelName string) (int, error)
	EstimateMessages(msgs []model.Message, modelName string) (int, error)
}

// TiktokenEstimator uses offline BPE algorithms to estimate tokens.
// messageOverhead and assistantPrimeOverhead match OpenAI ChatML framing; both
// default to 0 for providers that do not use ChatML (Gemini, Anthropic, Ollama).
// All fields are set at construction; do not mutate after first concurrent use.
type TiktokenEstimator struct {
	logger                 *slog.Logger
	encodingCache          sync.Map // map[string]*tiktoken.Tiktoken
	sfg                    singleflight.Group
	messageOverhead        int
	assistantPrimeOverhead int
}

// EstimatorOption configures a TiktokenEstimator.
type EstimatorOption func(*TiktokenEstimator)

// WithChatMLOverheads sets OpenAI ChatML per-message and assistant-prime token
// overheads (typically 4 and 3 respectively).
func WithChatMLOverheads(perMessage, assistantPrime int) EstimatorOption {
	return func(e *TiktokenEstimator) {
		e.messageOverhead = perMessage
		e.assistantPrimeOverhead = assistantPrime
	}
}

// NewEstimator creates a new TiktokenEstimator. Pass WithChatMLOverheads to
// enable ChatML framing for OpenAI-compatible providers.
func NewEstimator(logger *slog.Logger, opts ...EstimatorOption) *TiktokenEstimator {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	e := &TiktokenEstimator{logger: logger}
	for _, o := range opts {
		o(e)
	}
	return e
}

// getEncoding returns a cached tiktoken encoding for the given model name,
// falling back to cl100k_base when the model is not known to tiktoken.
// Concurrent misses for the same model are coalesced into a single call.
func (e *TiktokenEstimator) getEncoding(modelName string) (*tiktoken.Tiktoken, error) {
	if v, ok := e.encodingCache.Load(modelName); ok {
		return v.(*tiktoken.Tiktoken), nil
	}

	v, err, _ := e.sfg.Do(modelName, func() (any, error) {
		// Re-check under the singleflight barrier — a prior call may have populated the cache.
		if cached, ok := e.encodingCache.Load(modelName); ok {
			return cached, nil
		}

		tkm, err := tiktoken.EncodingForModel(modelName)
		if err != nil {
			e.logger.Warn("model not found in tiktoken, falling back to cl100k_base", "model", modelName)
			tkm, err = tiktoken.GetEncoding("cl100k_base")
			if err != nil {
				return nil, fmt.Errorf("failed to get base encoding for token estimation: %w", err)
			}
		}

		e.encodingCache.Store(modelName, tkm)
		return tkm, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*tiktoken.Tiktoken), nil
}

// EstimateString calculates the tokens for a standard string payload.
func (e *TiktokenEstimator) EstimateString(text, modelName string) (int, error) {
	tkm, err := e.getEncoding(modelName)
	if err != nil {
		return 0, err
	}
	tokens := tkm.Encode(text, nil, nil)
	return len(tokens), nil
}

// EstimateMessage calculates tokens for an individual model.Message.
func (e *TiktokenEstimator) EstimateMessage(msg model.Message, modelName string) (int, error) {
	tokens := e.messageOverhead

	for _, part := range msg.GetParts().Values() {
		if contentStr, ok := part.GetContent().(string); ok {
			t, err := e.EstimateString(contentStr, modelName)
			if err != nil {
				return 0, err
			}
			tokens += t
		}
	}

	return tokens, nil
}

// EstimateMessages calculates tokens for a chronological array of model.Message items.
func (e *TiktokenEstimator) EstimateMessages(msgs []model.Message, modelName string) (int, error) {
	total := 0
	for _, msg := range msgs {
		t, err := e.EstimateMessage(msg, modelName)
		if err != nil {
			return 0, err
		}
		total += t
	}

	total += e.assistantPrimeOverhead
	return total, nil
}
