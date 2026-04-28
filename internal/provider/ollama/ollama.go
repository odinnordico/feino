package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ollama/ollama/api"

	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/provider"
	"github.com/odinnordico/feino/internal/structs"
)

// ProviderID is the canonical identifier for this provider.
const ProviderID = "ollama"

type Provider struct {
	client         *api.Client
	logger         *slog.Logger
	retryConfig    provider.RetryConfig
	circuitBreaker *provider.CircuitBreaker

	baseURL    string
	httpClient *http.Client
	isTesting  bool // to skip start-up logic in tests

	mu            sync.RWMutex
	selectedModel model.Model
	metrics       provider.Metrics
}

// ollamaRetryConfig returns retry settings tuned for local Ollama inference.
// The extended TotalTimeout accommodates cold-start model loading, which can
// take several minutes depending on model size and available GPU memory.
func ollamaRetryConfig() provider.RetryConfig {
	return provider.RetryConfig{
		MaxRetries:   3,
		TotalTimeout: 5 * time.Minute,
		InitialDelay: 2 * time.Second,
		MaxDelay:     30 * time.Second,
	}
}

func NewProvider(ctx context.Context, logger *slog.Logger) (*Provider, error) {
	if logger == nil {
		logger = slog.Default()
	}

	p := &Provider{
		logger:         logger,
		retryConfig:    ollamaRetryConfig(),
		circuitBreaker: provider.DefaultCircuitBreaker(logger.With("component", "circuit_breaker", "provider", "ollama")),
	}

	client, err := p.createAndEnsureClient(ctx)
	if err != nil {
		return nil, err
	}
	p.client = client

	return p, nil
}

// createAndEnsureClient checks if ollama is running, starts it if not, and creates a client
func (p *Provider) createAndEnsureClient(ctx context.Context) (*api.Client, error) {
	if !p.isTesting {
		// Check if ollama is installed
		path, err := exec.LookPath("ollama")
		if err != nil {
			return nil, fmt.Errorf("ollama not found in path: %w", err)
		}
		p.logger.Debug("ollama found in path", "path", path)

		// Check if ollama server is running (default port 11434)
		timeout := 1 * time.Second

		newCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		dialer := &net.Dialer{
			Timeout: timeout,
		}

		conn, err := dialer.DialContext(newCtx, "tcp", "localhost:11434")
		if err != nil {
			p.logger.Info("ollama server is not running, attempting to start it...")
			cmd := exec.CommandContext(ctx, "ollama", "serve")
			err = cmd.Start()
			if err != nil {
				return nil, fmt.Errorf("failed to start ollama server: %w", err)
			}
			// Wait for server to start
			for range 10 {
				time.Sleep(1 * time.Second)
				dialer2 := &net.Dialer{
					Timeout: timeout,
				}

				conn, err = dialer2.DialContext(newCtx, "tcp", "localhost:11434")
				if err == nil {
					cerr := conn.Close()
					if cerr != nil {
						p.logger.Error("ollama: failed to close connection", "error", cerr)
					}
					break
				}
			}
			if err != nil {
				return nil, fmt.Errorf("ollama server failed to start after 10 seconds")
			}
		} else {
			err = conn.Close()
			if err != nil {
				p.logger.Error("ollama: failed to close connection", "error", err)
			}
		}
	}

	if p.baseURL != "" {
		baseURL, err := url.Parse(p.baseURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse baseURL: %w", err)
		}
		httpClient := p.httpClient
		if httpClient == nil {
			httpClient = http.DefaultClient
		}
		return api.NewClient(baseURL, httpClient), nil
	}

	client, err := api.ClientFromEnvironment()
	if err != nil {
		return nil, fmt.Errorf("failed to create ollama client: %w", err)
	}

	return client, nil
}

// renewClient re-runs the ollama health checks, attempts to restart the server if it crashed,
// and recreates the client connection.
func (p *Provider) renewClient(ctx context.Context) error {
	p.logger.Info("renewing ollama client, checking server health...")
	client, err := p.createAndEnsureClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to renew ollama client: %w", err)
	}
	p.mu.Lock()
	p.client = client
	p.mu.Unlock()
	p.logger.Info("ollama client renewed")
	return nil
}

func (p *Provider) GetLogger() *slog.Logger {
	return p.logger
}

func (p *Provider) GetCircuitBreaker() *provider.CircuitBreaker {
	return p.circuitBreaker
}

func (p *Provider) GetName() string {
	return "Ollama"
}

func (p *Provider) GetID() string {
	return "ollama"
}

func (p *Provider) GetDescription() string {
	return "Run Llama 3, Mistral, Gemma, and other large language models locally."
}

func (p *Provider) GetHomepage() string {
	return "https://ollama.com/"
}

func (p *Provider) GetMetrics() *provider.Metrics {
	return &p.metrics
}

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

	resp, err := client.List(ctx)
	if err != nil {
		p.logger.Error("ollama: failed to list models", "error", err)
		return nil, fmt.Errorf("failed to list models: %w", err)
	}

	var models []model.Model
	for _, m := range resp.Models { //nolint:gocritic // model list is parsed once at startup; copy overhead is negligible
		models = append(models, &Model{
			id:       m.Name,
			name:     m.Name,
			provider: p,
			logger:   p.logger.With("model_id", m.Name),
		})
	}
	p.logger.Debug("ollama: listed models", "count", len(models))
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

type Model struct {
	id       string
	name     string
	provider *Provider
	logger   *slog.Logger
}

func (m *Model) GetID() string {
	return m.id
}

func (m *Model) GetName() string {
	return m.name
}

func (m *Model) GetDescription() string {
	return fmt.Sprintf("Ollama model: %s", m.id)
}

func (m *Model) GetHomepage() string {
	return m.provider.GetHomepage()
}

func (m *Model) GetLogger() *slog.Logger {
	return m.logger
}

func (m *Model) GetContextWindow() int   { return 0 }
func (m *Model) GetMaxOutputTokens() int { return 0 }
func (m *Model) SupportsTools() bool     { return true }

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
	m.logger.Info("ollama: starting inference",
		"model", m.id,
		"history_messages", len(history),
		"tools", len(opts.Tools),
	)

	messages := make([]api.Message, 0, len(history))
	for _, msg := range history {
		var content strings.Builder
		var toolCalls []api.ToolCall

		for p := range msg.GetParts().Iterator() {
			switch v := p.GetContent().(type) {
			case string:
				content.WriteString(v)
			case model.ToolCall:
				var args api.ToolCallFunctionArguments
				if err := json.Unmarshal([]byte(v.Arguments), &args); err != nil {
					m.GetLogger().Error("failed to unmarshal tool call arguments", "error", err)
				}
				toolCalls = append(toolCalls, api.ToolCall{
					Function: api.ToolCallFunction{
						Name:      v.Name,
						Arguments: args,
					},
				})
			case model.ToolResult:
				content.Reset()
				content.WriteString(v.Content)
			}
		}

		role := "user"
		switch msg.GetRole() {
		case model.MessageRoleAssistant:
			role = "assistant"
		case model.MessageRoleSystem:
			role = "system"
		case model.MessageRoleTool:
			role = "tool"
		}

		messages = append(messages, api.Message{
			Role:      role,
			Content:   content.String(),
			ToolCalls: toolCalls,
		})
	}

	options := make(map[string]any)
	if opts.Config.Temperature != nil {
		options["temperature"] = *opts.Config.Temperature
	}
	if opts.Config.TopP != nil {
		options["top_p"] = *opts.Config.TopP
	}
	if opts.Config.MaxOutputTokens != nil {
		options["num_predict"] = *opts.Config.MaxOutputTokens
	}
	if len(opts.Config.StopSequences) > 0 {
		options["stop"] = opts.Config.StopSequences
	}

	stream := true
	req := &api.ChatRequest{
		Model:    m.id,
		Messages: messages,
		Stream:   &stream,
		Options:  options,
	}

	// Map Tools
	if len(opts.Tools) > 0 {
		ollamaTools := make([]api.Tool, 0, len(opts.Tools))
		for _, t := range opts.Tools {
			var params api.ToolFunctionParameters
			paramsJSON, _ := json.Marshal(t.GetParameters())
			_ = json.Unmarshal(paramsJSON, &params)

			ollamaTools = append(ollamaTools, api.Tool{
				Type: "function",
				Function: api.ToolFunction{
					Name:        t.GetName(),
					Description: t.GetDescription(),
					Parameters:  params,
				},
			})
		}
		req.Tools = ollamaTools
	}

	resParts := structs.NewLinkedList[model.MessagePart]()
	if onPart == nil {
		onPart = func(p model.MessagePart) {}
	}

	var usage model.Usage
	fn := func(resp api.ChatResponse) error {
		if resp.Message.Content != "" {
			part := model.NewTextMessagePart(model.MessageRoleAssistant, resp.Message.Content)
			resParts.PushBack(part)
			onPart(part)
		}

		// Handle Tool Calls from Ollama
		if len(resp.Message.ToolCalls) > 0 {
			for _, tc := range resp.Message.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Function.Arguments)
				tCall := model.ToolCall{
					ID:        uuid.New().String(),
					Name:      tc.Function.Name,
					Arguments: string(argsJSON),
				}
				part := model.NewToolCallPart(tCall)
				resParts.PushBack(part)
				onPart(part)
			}
		}

		if resp.Done {
			usage.PromptTokens = resp.PromptEvalCount
			usage.CompletionTokens = resp.EvalCount
			usage.TotalTokens = resp.PromptEvalCount + resp.EvalCount
			usage.Duration = resp.TotalDuration
		}
		return nil
	}

	m.provider.mu.RLock()
	client := m.provider.client
	m.provider.mu.RUnlock()

	err := client.Chat(ctx, req, fn)
	if err != nil {
		m.logger.Error("ollama: chat request failed",
			"model", m.id,
			"history_messages", len(history),
			"tools", len(opts.Tools),
			"error", err,
		)
		return nil, usage, fmt.Errorf("failed to chat with ollama: %w", err)
	}

	if resParts.IsEmpty() {
		m.logger.Error("ollama: empty response",
			"model", m.id,
			"prompt_tokens", usage.PromptTokens,
			"completion_tokens", usage.CompletionTokens,
			"duration", usage.Duration,
		)
		return nil, usage, fmt.Errorf("no response from ollama")
	}

	m.logger.Info("ollama: inference complete",
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
