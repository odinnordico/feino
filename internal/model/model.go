package model

import (
	"context"
	"log/slog"
	"time"
)

// Usage tracks the token consumption and performance metadata for an inference call.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	// Support for prompt caching
	CacheCreationTokens int
	CacheReadTokens     int
	Duration            time.Duration
}

// GenerationConfig allows fine-tuning the model's output at request time.
type GenerationConfig struct {
	Temperature     *float32
	TopP            *float32
	MaxOutputTokens *int
	StopSequences   []string
}

// InferOptions encapsulates all parameters for a single inference call.
type InferOptions struct {
	Config GenerationConfig
	Tools  []Tool
}

// Tool is a shadow interface to avoid circular dependencies with internal/tools.
type Tool interface {
	GetName() string
	GetDescription() string
	// GetParameters returns a JSON Schema-compatible map.
	GetParameters() map[string]any
}

// Model defines the capabilities and interaction surface for an LLM provider.
type Model interface {
	GetID() string
	GetName() string
	GetDescription() string
	GetHomepage() string
	GetLogger() *slog.Logger

	// Capabilities
	GetContextWindow() int   // Max input tokens
	GetMaxOutputTokens() int // Max generation tokens
	SupportsTools() bool     // Indicates if the model can perform structured tool calls

	// Core Inference
	Infer(ctx context.Context, history []Message, opts InferOptions, onPart func(MessagePart)) (Message, Usage, error)
}
