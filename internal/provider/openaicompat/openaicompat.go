// Package openaicompat implements a provider for any server that speaks the
// OpenAI Chat Completions REST API — vLLM, LocalAI, LM Studio, Llamafile,
// Jan, and similar local or self-hosted inference servers.
//
// Key differences from the standard OpenAI provider:
//   - BaseURL is required (the endpoint is not fixed).
//   - APIKey is optional — many local servers do not require authentication.
//     When empty the client sends "none" as the Bearer token, which vLLM and
//     most compatible servers accept without complaint.
//   - GetName / GetID reflect user-supplied labels so multiple instances can
//     coexist (e.g. a vLLM endpoint and a LocalAI endpoint at the same time).
package openaicompat

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/shared"

	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/provider"
	"github.com/odinnordico/feino/internal/structs"
)

// Config holds the configuration for a generic OpenAI-compatible provider.
type Config struct {
	// BaseURL is the root of the API endpoint, e.g. "http://localhost:8000/v1".
	// It must be non-empty.
	BaseURL string

	// APIKey is the Bearer token sent in the Authorization header.
	// Leave empty for servers that do not require authentication (vLLM in
	// no-auth mode, LocalAI, LM Studio, etc.). When empty the string "none"
	// is sent, which most compatible servers silently accept.
	APIKey string

	// Name is the human-readable label shown in the TUI and logs.
	// Defaults to "OpenAI-Compatible" when empty.
	Name string

	// DisableTools prevents the provider from advertising tool/function-calling
	// support. Set this when connecting to a model or server that does not
	// implement the `tools` field in chat completions (e.g. older llama.cpp
	// builds, pure completion endpoints, or embedding-only servers).
	// Default false — tools are enabled.
	DisableTools bool
}

// ProviderID is the canonical identifier for this provider.
const ProviderID = "openai_compat"

// Provider implements provider.Provider for any OpenAI-compatible server.
type Provider struct {
	cfg            Config
	client         *openai.Client
	logger         *slog.Logger
	retryConfig    provider.RetryConfig
	circuitBreaker *provider.CircuitBreaker

	// httpClient and baseURL are used by tests to inject a mock transport.
	httpClient *http.Client
	baseURL    string // overrides cfg.BaseURL when set

	mu            sync.RWMutex
	selectedModel model.Model
	metrics       provider.Metrics
}

// NewProvider constructs a Provider from cfg. It returns an error when
// cfg.BaseURL is empty.
func NewProvider(ctx context.Context, cfg Config, logger *slog.Logger) (*Provider, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("openai_compat: BaseURL is required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	// Fall back to environment variable when the config carries no key.
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("OPENAI_COMPAT_API_KEY")
	}

	p := &Provider{
		cfg:            cfg,
		logger:         logger,
		retryConfig:    provider.DefaultRetryConfig(),
		circuitBreaker: provider.DefaultCircuitBreaker(logger.With("component", "circuit_breaker", "provider", "openai_compat")),
	}
	p.client = p.createClient()
	return p, nil
}

// createClient builds an openai.Client pointing at cfg.BaseURL (or the
// test-injected baseURL field when set).
func (p *Provider) createClient() *openai.Client {
	apiKey := p.cfg.APIKey
	if apiKey == "" {
		// OpenAI SDK rejects an empty key. "none" is widely accepted by
		// compatible servers that run without authentication.
		apiKey = "none"
	}

	effectiveURL := p.cfg.BaseURL
	if p.baseURL != "" {
		effectiveURL = p.baseURL
	}
	// Ensure the URL ends with a slash so the SDK appends paths correctly.
	if !strings.HasSuffix(effectiveURL, "/") {
		effectiveURL += "/"
	}

	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithBaseURL(effectiveURL),
		option.WithMaxRetries(0), // retries are handled by provider.Retry
	}
	if p.httpClient != nil {
		opts = append(opts, option.WithHTTPClient(p.httpClient))
	}

	c := openai.NewClient(opts...)
	return &c
}

// renewClient recreates the SDK client, re-reading the API key from the
// environment in case it was rotated.
func (p *Provider) renewClient(_ context.Context) error {
	if key := os.Getenv("OPENAI_COMPAT_API_KEY"); key != "" {
		p.mu.Lock()
		p.cfg.APIKey = key
		p.mu.Unlock()
	}
	client := p.createClient()
	p.mu.Lock()
	p.client = client
	p.mu.Unlock()
	p.logger.Info("openai_compat client renewed")
	return nil
}

// ── provider.Provider interface ───────────────────────────────────────────────

func (p *Provider) GetID() string { return "openai_compat" }

func (p *Provider) GetName() string {
	if p.cfg.Name != "" {
		return p.cfg.Name
	}
	return "OpenAI-Compatible"
}

func (p *Provider) GetDescription() string {
	return fmt.Sprintf("OpenAI-compatible endpoint at %s (vLLM, LocalAI, LM Studio, …)", p.cfg.BaseURL)
}

func (p *Provider) GetHomepage() string {
	return p.cfg.BaseURL
}

func (p *Provider) GetLogger() *slog.Logger { return p.logger }

func (p *Provider) GetCircuitBreaker() *provider.CircuitBreaker { return p.circuitBreaker }

func (p *Provider) GetMetrics() *provider.Metrics { return &p.metrics }

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

// GetModels lists models available on the remote endpoint via GET /models,
// wrapped with retry and circuit-breaker logic.
func (p *Provider) GetModels(ctx context.Context) ([]model.Model, error) {
	return provider.Retry(ctx, p.retryConfig, p.circuitBreaker, &p.metrics, p.logger, p.renewClient, p.getModelsInternal)
}

func (p *Provider) getModelsInternal(ctx context.Context) ([]model.Model, error) {
	p.mu.RLock()
	client := p.client
	p.mu.RUnlock()

	page, err := client.Models.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("openai_compat: list models: %w", err)
	}

	out := make([]model.Model, 0, len(page.Data))
	for _, m := range page.Data { //nolint:gocritic // model list parsed once at startup; copy overhead is negligible
		out = append(out, &Model{
			id:       m.ID,
			provider: p,
			logger:   p.logger.With("model_id", m.ID),
		})
	}
	p.logger.Debug("openai_compat: models fetched", "count", len(out))
	return out, nil
}

// ── Model ─────────────────────────────────────────────────────────────────────

// Model implements model.Model for a single model on an OpenAI-compatible server.
type Model struct {
	id       string
	provider *Provider
	logger   *slog.Logger
}

func (m *Model) GetID() string   { return m.id }
func (m *Model) GetName() string { return m.id }
func (m *Model) GetDescription() string {
	return fmt.Sprintf("Model %s on %s", m.id, m.provider.GetName())
}
func (m *Model) GetHomepage() string     { return m.provider.cfg.BaseURL }
func (m *Model) GetLogger() *slog.Logger { return m.logger }
func (m *Model) GetContextWindow() int   { return 0 }
func (m *Model) GetMaxOutputTokens() int { return 0 }
func (m *Model) SupportsTools() bool     { return !m.provider.cfg.DisableTools }

// Infer sends a streaming chat-completion request, wrapped with retry and
// circuit-breaker logic.
func (m *Model) Infer(ctx context.Context, history []model.Message, opts model.InferOptions, onPart func(model.MessagePart)) (model.Message, model.Usage, error) {
	type inferResult struct {
		msg   model.Message
		usage model.Usage
	}
	result, err := provider.Retry(ctx, m.provider.retryConfig, m.provider.circuitBreaker, &m.provider.metrics, m.logger, m.provider.renewClient, func(rctx context.Context) (inferResult, error) {
		msg, usage, err := m.inferInternal(rctx, history, opts, onPart)
		return inferResult{msg: msg, usage: usage}, err
	})
	if err != nil {
		return nil, result.usage, err
	}
	return result.msg, result.usage, nil
}

func (m *Model) inferInternal(ctx context.Context, history []model.Message, opts model.InferOptions, onPart func(model.MessagePart)) (model.Message, model.Usage, error) {
	start := time.Now()
	m.logger.Info("openai_compat: starting inference",
		"model", m.id,
		"history_messages", len(history),
		"tools", len(opts.Tools),
	)

	// Build the OpenAI message slice from internal history.
	var messages []openai.ChatCompletionMessageParamUnion
	for _, msg := range history {
		if msg.GetParts() == nil {
			continue
		}

		var text strings.Builder
		var toolCalls []openai.ChatCompletionMessageToolCallParam
		var toolResults []openai.ChatCompletionMessageParamUnion

		for p := range msg.GetParts().Iterator() {
			switch v := p.GetContent().(type) {
			case string:
				text.WriteString(v)
			case model.ToolCall:
				toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallParam{
					ID:   v.ID,
					Type: "function",
					Function: openai.ChatCompletionMessageToolCallFunctionParam{
						Name:      v.Name,
						Arguments: v.Arguments,
					},
				})
			case model.ToolResult:
				content := v.Content
				if v.IsError {
					content = "ERROR: " + content
				}
				toolResults = append(toolResults, openai.ToolMessage(content, v.CallID))
			}
		}

		if len(toolResults) > 0 {
			messages = append(messages, toolResults...)
			continue
		}

		switch msg.GetRole() {
		case model.MessageRoleSystem:
			messages = append(messages, openai.SystemMessage(text.String()))
		case model.MessageRoleAssistant:
			if len(toolCalls) > 0 {
				messages = append(messages, openai.ChatCompletionMessageParamUnion{
					OfAssistant: &openai.ChatCompletionAssistantMessageParam{
						Content:   openai.ChatCompletionAssistantMessageParamContentUnion{OfString: param.NewOpt(text.String())},
						ToolCalls: toolCalls,
					},
				})
			} else {
				messages = append(messages, openai.AssistantMessage(text.String()))
			}
		default:
			messages = append(messages, openai.UserMessage(text.String()))
		}
	}

	params := openai.ChatCompletionNewParams{
		Model:    m.id,
		Messages: messages,
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		},
	}

	// Apply generation config.
	if opts.Config.Temperature != nil {
		params.Temperature = openai.Float(float64(*opts.Config.Temperature))
	}
	if opts.Config.TopP != nil {
		params.TopP = openai.Float(float64(*opts.Config.TopP))
	}
	if opts.Config.MaxOutputTokens != nil {
		params.MaxTokens = openai.Int(int64(*opts.Config.MaxOutputTokens))
	}
	if len(opts.Config.StopSequences) > 0 {
		params.Stop = openai.ChatCompletionNewParamsStopUnion{OfStringArray: opts.Config.StopSequences}
	}

	// Register tools.
	if len(opts.Tools) > 0 {
		oaiTools := make([]openai.ChatCompletionToolParam, 0, len(opts.Tools))
		for _, t := range opts.Tools {
			oaiTools = append(oaiTools, openai.ChatCompletionToolParam{
				Type: "function",
				Function: shared.FunctionDefinitionParam{
					Name:        t.GetName(),
					Description: openai.String(t.GetDescription()),
					Parameters:  shared.FunctionParameters(t.GetParameters()),
				},
			})
		}
		params.Tools = oaiTools
	}

	m.provider.mu.RLock()
	client := m.provider.client
	m.provider.mu.RUnlock()
	stream := client.Chat.Completions.NewStreaming(ctx, params)

	parts := structs.NewLinkedList[model.MessagePart]()
	if onPart == nil {
		onPart = func(model.MessagePart) {}
	}

	var usage model.Usage

	type toolCallBuilder struct {
		id   string
		name string
		args strings.Builder
	}
	tcByIdx := make(map[int64]*toolCallBuilder)

	for stream.Next() {
		chunk := stream.Current()

		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta
			if delta.Content != "" {
				part := model.NewTextMessagePart(model.MessageRoleAssistant, delta.Content)
				parts.PushBack(part)
				onPart(part)
			}
			for _, tc := range delta.ToolCalls { //nolint:gocritic // streaming response tool calls parsed once per request; copy overhead is negligible
				b, ok := tcByIdx[tc.Index]
				if !ok {
					b = &toolCallBuilder{}
					tcByIdx[tc.Index] = b
				}
				if tc.ID != "" {
					b.id = tc.ID
				}
				if tc.Function.Name != "" {
					b.name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					b.args.WriteString(tc.Function.Arguments)
				}
			}
		}

		if chunk.Usage.TotalTokens > 0 {
			usage.PromptTokens = int(chunk.Usage.PromptTokens)
			usage.CompletionTokens = int(chunk.Usage.CompletionTokens)
			usage.TotalTokens = int(chunk.Usage.TotalTokens)
		}
	}

	// Finalize tool calls in index order.
	indices := make([]int64, 0, len(tcByIdx))
	for idx := range tcByIdx {
		indices = append(indices, idx)
	}
	slices.Sort(indices)
	for _, idx := range indices {
		b := tcByIdx[idx]
		tc := model.ToolCall{ID: b.id, Name: b.name, Arguments: b.args.String()}
		part := model.NewToolCallPart(tc)
		parts.PushBack(part)
		onPart(part)
	}

	if err := stream.Err(); err != nil {
		m.logger.Error("openai_compat: stream error", "model", m.id, "error", err)
		return nil, usage, fmt.Errorf("openai_compat: stream error: %w", err)
	}

	if parts.IsEmpty() {
		m.logger.Error("openai_compat: empty response", "model", m.id, "prompt_tokens", usage.PromptTokens)
		return nil, usage, fmt.Errorf("openai_compat: empty response from %s", m.provider.GetName())
	}

	usage.Duration = time.Since(start)

	m.logger.Info("openai_compat: inference complete",
		"model", m.id,
		"prompt_tokens", usage.PromptTokens,
		"completion_tokens", usage.CompletionTokens,
		"tool_calls", len(indices),
		"duration", usage.Duration,
	)

	return model.NewMessage(
		model.WithRole(model.MessageRoleAssistant),
		model.WithContent(parts),
	), usage, nil
}
