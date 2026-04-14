package openai

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

// ProviderID is the canonical identifier for this provider.
const ProviderID = "openai"

// Provider implements the model.Provider interface using the official OpenAI Go SDK
type Provider struct {
	client *openai.Client
	apiKey string

	logger         *slog.Logger
	retryConfig    provider.RetryConfig
	circuitBreaker *provider.CircuitBreaker

	baseURL    string
	httpClient *http.Client

	mu            sync.RWMutex
	selectedModel model.Model
	metrics       provider.ProviderMetrics
}

// NewProvider constructs an OpenAI provider. apiKey is used directly when
// non-empty; otherwise it falls back to the OPENAI_API_KEY environment variable.
func NewProvider(ctx context.Context, apiKey string, logger *slog.Logger) (*Provider, error) {
	if logger == nil {
		logger = slog.Default()
	}

	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable is required")
	}

	p := &Provider{
		logger:         logger,
		apiKey:         apiKey,
		retryConfig:    provider.DefaultRetryConfig(),
		circuitBreaker: provider.DefaultCircuitBreaker(logger.With("component", "circuit_breaker", "provider", "openai")),
	}
	p.client = p.createClient()

	return p, nil
}

// createClient builds an openai.Client using the provider's configuration
func (p *Provider) createClient() *openai.Client {
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

	client := openai.NewClient(opts...)
	return &client
}

func (p *Provider) GetName() string { return "OpenAI" }
func (p *Provider) GetID() string   { return "openai" }
func (p *Provider) GetDescription() string {
	return "OpenAI language models including GPT-4 and o-series."
}
func (p *Provider) GetHomepage() string     { return "https://platform.openai.com/" }
func (p *Provider) GetLogger() *slog.Logger { return p.logger }

func (p *Provider) SetModel(m model.Model) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.selectedModel = m
}

// renewClient recreates the OpenAI SDK client, re-reading the API key from
// the environment if the stored key fails authentication
func (p *Provider) renewClient(ctx context.Context) error {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		p.mu.RLock()
		apiKey = p.apiKey
		p.mu.RUnlock()
	}
	// Build the new client outside the lock to avoid holding it during I/O.
	p.mu.Lock()
	p.apiKey = apiKey
	p.mu.Unlock()
	client := p.createClient()
	p.mu.Lock()
	p.client = client
	p.mu.Unlock()
	p.logger.Info("openai client renewed")
	return nil
}

func (p *Provider) GetModels(ctx context.Context) ([]model.Model, error) {
	return provider.Retry(ctx, p.retryConfig, p.circuitBreaker, &p.metrics, p.logger, p.renewClient, p.getModelsInternal)
}

func (p *Provider) getModelsInternal(ctx context.Context) ([]model.Model, error) {
	p.mu.RLock()
	client := p.client
	p.mu.RUnlock()

	page, err := client.Models.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("openai: list models: %w", err)
	}

	var parsedModels []model.Model
	for _, m := range page.Data {
		parsedModels = append(parsedModels, &Model{
			id:       m.ID,
			provider: p,
			logger:   p.logger,
		})
	}

	return parsedModels, nil
}

func (p *Provider) GetSelectedModel() model.Model {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.selectedModel
}

// Model implements the model.Model interface for OpenAI
type Model struct {
	id       string
	provider *Provider
	logger   *slog.Logger
}

func (m *Model) GetID() string           { return m.id }
func (m *Model) GetName() string         { return m.id }
func (m *Model) GetDescription() string  { return "OpenAI language model" }
func (m *Model) GetHomepage() string     { return "https://platform.openai.com/docs/models" }
func (m *Model) GetLogger() *slog.Logger { return m.logger }

func (m *Model) GetContextWindow() int   { return 0 }
func (m *Model) GetMaxOutputTokens() int { return 0 }
func (m *Model) SupportsTools() bool     { return true }

func (p *Provider) GetCircuitBreaker() *provider.CircuitBreaker {
	return p.circuitBreaker
}

func (p *Provider) GetMetrics() *provider.ProviderMetrics {
	return &p.metrics
}

func (m *Model) Infer(ctx context.Context, history []model.Message, opts model.InferOptions, onPart func(model.MessagePart)) (model.Message, model.Usage, error) {
	type inferResult struct {
		msg   model.Message
		usage model.Usage
	}

	result, err := provider.Retry(ctx, m.provider.retryConfig, m.provider.circuitBreaker, &m.provider.metrics, m.logger, m.provider.renewClient, func(retryCtx context.Context) (inferResult, error) {
		msg, usage, err := m.inferInternal(retryCtx, history, opts, onPart)
		return inferResult{msg: msg, usage: usage}, err
	})
	if err != nil {
		return nil, result.usage, err
	}
	return result.msg, result.usage, nil
}

func (m *Model) inferInternal(ctx context.Context, history []model.Message, opts model.InferOptions, onPart func(model.MessagePart)) (model.Message, model.Usage, error) {
	start := time.Now()
	if onPart == nil {
		onPart = func(model.MessagePart) {}
	}
	m.logger.Info("openai: starting inference",
		"model", m.id,
		"history_messages", len(history),
		"tools", len(opts.Tools),
	)

	m.provider.mu.RLock()
	client := m.provider.client
	m.provider.mu.RUnlock()

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
				toolResults = append(toolResults, openai.ToolMessage(v.Content, v.CallID))
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
		case model.MessageRoleUser:
			messages = append(messages, openai.UserMessage(text.String()))
		default:
			messages = append(messages, openai.UserMessage(text.String()))
		}
	}

	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(m.id),
		Messages: messages,
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		},
	}

	// Apply Generation Config
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

	// Register Tools
	if len(opts.Tools) > 0 {
		var openaiTools []openai.ChatCompletionToolParam
		for _, t := range opts.Tools {
			openaiTools = append(openaiTools, openai.ChatCompletionToolParam{
				Type: "function",
				Function: shared.FunctionDefinitionParam{
					Name:        t.GetName(),
					Description: openai.String(t.GetDescription()),
					Parameters:  shared.FunctionParameters(t.GetParameters()),
				},
			})
		}
		params.Tools = openaiTools
	}

	stream := client.Chat.Completions.NewStreaming(ctx, params)

	parts := structs.NewLinkedList[model.MessagePart]()
	var currentUsage model.Usage

	type toolCallBuilder struct {
		id   string
		name string
		args strings.Builder
	}
	toolCallsByIdx := make(map[int64]*toolCallBuilder)

	for stream.Next() {
		chunk := stream.Current()

		if len(chunk.Choices) > 0 {
			choice := chunk.Choices[0]
			content := choice.Delta.Content
			if content != "" {
				part := model.NewTextMessagePart(model.MessageRoleAssistant, content)
				parts.PushBack(part)
				onPart(part)
			}

			// Capture Tool Calls
			for _, tc := range choice.Delta.ToolCalls {
				builder, ok := toolCallsByIdx[tc.Index]
				if !ok {
					builder = &toolCallBuilder{}
					toolCallsByIdx[tc.Index] = builder
				}
				if tc.ID != "" {
					builder.id = tc.ID
				}
				if tc.Function.Name != "" {
					builder.name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					builder.args.WriteString(tc.Function.Arguments)
				}
			}
		}

		if chunk.Usage.TotalTokens > 0 {
			currentUsage.PromptTokens = int(chunk.Usage.PromptTokens)
			currentUsage.CompletionTokens = int(chunk.Usage.CompletionTokens)
			currentUsage.TotalTokens = int(chunk.Usage.TotalTokens)
		}
	}

	// Finalize Tool Calls
	var builderIndices []int64
	for idx := range toolCallsByIdx {
		builderIndices = append(builderIndices, idx)
	}
	slices.Sort(builderIndices)

	for _, idx := range builderIndices {
		b := toolCallsByIdx[idx]
		part := model.NewToolCallPart(model.ToolCall{
			ID:        b.id,
			Name:      b.name,
			Arguments: b.args.String(),
		})
		parts.PushBack(part)
		onPart(part)
	}

	if err := stream.Err(); err != nil {
		m.logger.Error("openai: stream error", "model", m.id, "error", err)
		return nil, model.Usage{}, err
	}

	if parts.IsEmpty() {
		m.logger.Error("openai: empty response",
			"model", m.id,
			"prompt_tokens", currentUsage.PromptTokens,
		)
		return nil, model.Usage{}, fmt.Errorf("received empty response from OpenAI")
	}

	currentUsage.Duration = time.Since(start)

	m.logger.Info("openai: inference complete",
		"model", m.id,
		"prompt_tokens", currentUsage.PromptTokens,
		"completion_tokens", currentUsage.CompletionTokens,
		"tool_calls_finalized", len(builderIndices),
		"duration", currentUsage.Duration,
	)

	resultMessage := model.NewMessage(
		model.WithRole(model.MessageRoleAssistant),
		model.WithContent(parts),
	)

	return resultMessage, currentUsage, nil
}
