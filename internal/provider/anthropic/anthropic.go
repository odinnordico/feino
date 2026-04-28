package anthropic

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/provider"
	"github.com/odinnordico/feino/internal/structs"
)

// ProviderID is the canonical identifier for this provider.
const ProviderID = "anthropic"

// Provider implements the provider.Provider interface for Anthropic's Claude models
type Provider struct {
	client         anthropic.Client
	logger         *slog.Logger
	apiKey         string
	retryConfig    provider.RetryConfig
	circuitBreaker *provider.CircuitBreaker

	baseURL    string
	httpClient *http.Client

	mu            sync.RWMutex
	selectedModel model.Model
	metrics       provider.Metrics
}

// NewProvider creates a new Anthropic provider with the given API key and logger.
// If apiKey is empty, it falls back to the ANTHROPIC_API_KEY environment variable.
func NewProvider(ctx context.Context, apiKey string, logger *slog.Logger) (*Provider, error) {
	if logger == nil {
		logger = slog.Default()
	}

	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}

	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY or apiKey must be provided")
	}

	p := &Provider{
		logger:         logger,
		apiKey:         apiKey,
		retryConfig:    provider.DefaultRetryConfig(),
		circuitBreaker: provider.DefaultCircuitBreaker(logger.With("component", "circuit_breaker", "provider", "anthropic")),
	}

	p.client = p.createClient()

	return p, nil
}

// createClient builds an anthropic.Client using the provider's configuration
func (p *Provider) createClient() anthropic.Client {
	opts := []option.RequestOption{
		option.WithAPIKey(p.apiKey),
		option.WithMaxRetries(0),
	}

	if p.baseURL != "" {
		opts = append(opts, option.WithBaseURL(p.baseURL))
	}

	if p.httpClient != nil {
		opts = append(opts, option.WithHTTPClient(p.httpClient))
	}

	return anthropic.NewClient(opts...)
}

func (p *Provider) GetLogger() *slog.Logger {
	return p.logger
}

func (p *Provider) GetCircuitBreaker() *provider.CircuitBreaker {
	return p.circuitBreaker
}

// renewClient recreates the Anthropic SDK client, re-reading the API key from
// the environment if the stored key fails authentication
func (p *Provider) renewClient(ctx context.Context) error {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		p.mu.RLock()
		apiKey = p.apiKey
		p.mu.RUnlock()
	}
	client := p.createClientWithKey(apiKey)
	p.mu.Lock()
	p.apiKey = apiKey
	p.client = client
	p.mu.Unlock()
	p.logger.Info("anthropic client renewed")
	return nil
}

// createClientWithKey builds a client using the provided key.
// baseURL and httpClient are only written during test setup, so no lock is needed.
func (p *Provider) createClientWithKey(apiKey string) anthropic.Client {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithMaxRetries(0),
	}
	if p.baseURL != "" {
		opts = append(opts, option.WithBaseURL(p.baseURL))
	}
	if p.httpClient != nil {
		opts = append(opts, option.WithHTTPClient(p.httpClient))
	}
	return anthropic.NewClient(opts...)
}

func (p *Provider) GetName() string {
	return "Anthropic"
}

func (p *Provider) GetID() string {
	return "anthropic"
}

func (p *Provider) GetDescription() string {
	return "Anthropic's Claude models for high-performance AI tasks."
}

func (p *Provider) GetHomepage() string {
	return "https://www.anthropic.com/"
}

func (p *Provider) GetMetrics() *provider.Metrics {
	return &p.metrics
}

// GetModels fetches available models from the Anthropic API using auto-pagination,
// wrapped with retry and circuit breaker logic
func (p *Provider) GetModels(ctx context.Context) ([]model.Model, error) {
	return provider.Retry(ctx, p.retryConfig, p.circuitBreaker, &p.metrics, p.logger,
		p.renewClient,
		func(ctx context.Context) ([]model.Model, error) {
			return p.getModelsInternal(ctx)
		},
	)
}

func (p *Provider) getModelsInternal(ctx context.Context) ([]model.Model, error) {
	p.mu.RLock()
	client := p.client
	p.mu.RUnlock()

	var models []model.Model
	pager := client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})

	for pager.Next() {
		m := pager.Current()
		models = append(models, &Model{
			id:          m.ID,
			name:        m.DisplayName,
			description: fmt.Sprintf("Anthropic model: %s (max %d output tokens)", m.ID, m.MaxTokens),
			maxTokens:   m.MaxTokens,
			provider:    p,
			logger:      p.logger.With("model_id", m.ID),
		})
	}

	if err := pager.Err(); err != nil {
		return nil, fmt.Errorf("failed to list anthropic models: %w", err)
	}

	return models, nil
}

func (p *Provider) SetModel(m model.Model) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.selectedModel = m
}

func (p *Provider) GetSelectedModel() model.Model {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.selectedModel
}

// Model implements the model.Model interface for an Anthropic model
type Model struct {
	id          string
	name        string
	description string
	maxTokens   int64
	provider    *Provider
	logger      *slog.Logger
}

func (m *Model) GetID() string {
	return m.id
}

func (m *Model) GetName() string {
	return m.name
}

func (m *Model) GetDescription() string {
	return m.description
}

func (m *Model) GetHomepage() string {
	return m.provider.GetHomepage()
}

func (m *Model) GetLogger() *slog.Logger {
	return m.logger
}

func (m *Model) GetContextWindow() int {
	return 0
}

func (m *Model) GetMaxOutputTokens() int {
	return int(m.maxTokens)
}

func (m *Model) SupportsTools() bool {
	return true
}

// Infer sends a streaming message request to the Anthropic API,
// wrapped with retry and circuit breaker logic
func (m *Model) Infer(ctx context.Context, history []model.Message, opts model.InferOptions, onPart func(model.MessagePart)) (model.Message, model.Usage, error) {
	type inferResult struct {
		msg   model.Message
		usage model.Usage
	}

	result, err := provider.Retry(ctx, m.provider.retryConfig, m.provider.circuitBreaker, &m.provider.metrics, m.logger,
		m.provider.renewClient,
		func(ctx context.Context) (inferResult, error) {
			msg, usage, err := m.inferInternal(ctx, history, opts, onPart)
			return inferResult{msg: msg, usage: usage}, err
		},
	)
	if err != nil {
		return nil, result.usage, err
	}
	return result.msg, result.usage, nil
}

func (m *Model) inferInternal(ctx context.Context, history []model.Message, opts model.InferOptions, onPart func(model.MessagePart)) (model.Message, model.Usage, error) {
	start := time.Now()
	m.logger.Info("anthropic: starting inference",
		"model", m.id,
		"history_messages", len(history),
		"tools", len(opts.Tools),
	)

	var anthropicMessages []anthropic.MessageParam
	var systemBlocks []anthropic.TextBlockParam

	// Convert internal messages to Anthropic API format
	for _, msg := range history {
		var blocks []anthropic.ContentBlockParamUnion
		for p := range msg.GetParts().Iterator() {
			switch v := p.GetContent().(type) {
			case string:
				blocks = append(blocks, anthropic.NewTextBlock(v))
			case model.ToolCall:
				// Correctly map Tool Calls to Anthropic Tool Use blocks
				blocks = append(blocks, anthropic.NewToolUseBlock(v.ID, v.Arguments, v.Name))
			case model.ToolResult:
				// Correctly map Tool Results to Anthropic Tool Result blocks
				// Handle errors by marking the tool result as a failure
				resBlock := anthropic.NewToolResultBlock(v.CallID, v.Content, v.IsError)
				blocks = append(blocks, resBlock)
			default:
				m.GetLogger().Warn("unsupported message part content type", "type", reflect.TypeOf(v))
			}
		}

		// System messages are handled separately in the Anthropic API
		if msg.GetRole() == model.MessageRoleSystem {
			for _, b := range blocks { //nolint:gocritic // API response blocks are parsed once per request; copy overhead is negligible
				if txt := b.GetText(); txt != nil {
					systemBlocks = append(systemBlocks, anthropic.TextBlockParam{
						Text: *txt,
					})
				}
			}
			continue
		}

		if msg.GetRole() == model.MessageRoleAssistant {
			anthropicMessages = append(anthropicMessages, anthropic.NewAssistantMessage(blocks...))
		} else {
			anthropicMessages = append(anthropicMessages, anthropic.NewUserMessage(blocks...))
		}
	}

	// Use the model's max tokens, capped at a reasonable default
	maxTokens := m.maxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	params := anthropic.MessageNewParams{
		Model:     m.id,
		MaxTokens: maxTokens,
		Messages:  anthropicMessages,
	}

	if opts.Config.Temperature != nil {
		params.Temperature = anthropic.Float(float64(*opts.Config.Temperature))
	}
	if opts.Config.TopP != nil {
		params.TopP = anthropic.Float(float64(*opts.Config.TopP))
	}
	if len(opts.Config.StopSequences) > 0 {
		params.StopSequences = opts.Config.StopSequences
	}

	// Register Tools
	if len(opts.Tools) > 0 {
		anthropicTools := make([]anthropic.ToolUnionParam, 0, len(opts.Tools))
		for _, t := range opts.Tools {
			var schema anthropic.ToolInputSchemaParam
			paramsMap := t.GetParameters()
			if prop, ok := paramsMap["properties"]; ok {
				schema.Properties = prop
			}
			if req, ok := paramsMap["required"].([]string); ok {
				schema.Required = req
			}
			// GetParameters often returns a full schema including "type": "object".
			// We'll set it to "object" by default if not specified.
			schema.Type = "object"

			anthropicTools = append(anthropicTools, anthropic.ToolUnionParam{
				OfTool: &anthropic.ToolParam{
					Name:        t.GetName(),
					Description: anthropic.String(t.GetDescription()),
					InputSchema: schema,
				},
			})
		}
		params.Tools = anthropicTools
	}

	if len(systemBlocks) > 0 {
		params.System = systemBlocks
	}

	m.provider.mu.RLock()
	client := m.provider.client
	m.provider.mu.RUnlock()
	stream := client.Messages.NewStreaming(ctx, params)
	defer func() { _ = stream.Close() }()

	resParts := structs.NewLinkedList[model.MessagePart]()
	if onPart == nil {
		onPart = func(p model.MessagePart) {}
	}

	var usage model.Usage
	// acc accumulates the full message so tool_use blocks can be extracted
	// after streaming completes. Text deltas are forwarded immediately via onPart
	// for real-time display; tool calls are non-streamable and emitted at the end.
	var acc anthropic.Message

	// Process stream events using the union type pattern
	for stream.Next() {
		event := stream.Current()

		// Always accumulate — this handles tool_use / input_json_delta internally.
		if err := acc.Accumulate(event); err != nil {
			m.logger.Warn("anthropic: stream accumulate error", "error", err)
		}

		switch event.Type {
		case "message_start":
			usage.PromptTokens = int(event.Message.Usage.InputTokens)
			usage.CacheCreationTokens = int(event.Message.Usage.CacheCreationInputTokens)
			usage.CacheReadTokens = int(event.Message.Usage.CacheReadInputTokens)

		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta":
				part := model.NewTextMessagePart(model.MessageRoleAssistant, event.Delta.Text)
				resParts.PushBack(part)
				onPart(part)
			case "thinking_delta":
				part := model.NewThoughtPart(event.Delta.Thinking)
				resParts.PushBack(part)
				onPart(part)
			}

		case "message_delta":
			usage.CompletionTokens = int(event.Usage.OutputTokens)
		}
	}

	if err := stream.Err(); err != nil {
		m.logger.Error("anthropic: stream error",
			"model", m.id,
			"error", err,
		)
		return nil, usage, fmt.Errorf("anthropic stream error: %w", err)
	}

	// Extract tool_use blocks from the accumulated message. Text blocks were
	// already emitted as streaming parts above; only tool calls need this pass.
	for _, block := range acc.Content { //nolint:gocritic // API response content blocks are parsed once per stream; copy overhead is negligible
		if block.Type == "tool_use" {
			tc := model.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: string(block.Input),
			}
			part := model.NewToolCallPart(tc)
			resParts.PushBack(part)
			onPart(part)
		}
	}

	if resParts.IsEmpty() {
		m.logger.Error("anthropic: empty response — no text or tool_use blocks received",
			"model", m.id,
			"prompt_tokens", usage.PromptTokens,
		)
		return nil, usage, fmt.Errorf("anthropic: empty response")
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	usage.Duration = time.Since(start)

	m.logger.Info("anthropic: inference complete",
		"model", m.id,
		"prompt_tokens", usage.PromptTokens,
		"completion_tokens", usage.CompletionTokens,
		"duration", usage.Duration,
	)

	return model.NewMessage(
		model.WithRole(model.MessageRoleAssistant),
		model.WithContent(resParts),
	), usage, nil
}
