package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"reflect"
	"sync"
	"time"

	"google.golang.org/genai"

	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/provider"
	"github.com/odinnordico/feino/internal/structs"
)

// AuthType represents the type of authentication to use
type AuthType string

const (
	AuthTypeAPIKey AuthType = "api-key"
	AuthTypeOAuth2 AuthType = "oauth2" // for Vertex AI / ADC
)

// Config represents the configuration for the Gemini provider
type Config struct {
	AuthType  AuthType
	APIKey    string
	ProjectID string
	Location  string
	Vertex    bool
}

// ProviderID is the canonical identifier for this provider.
const ProviderID = "gemini"

// Provider implements the provider.Provider interface for Google's Gemini models
type Provider struct {
	config         Config
	client         *genai.Client
	logger         *slog.Logger
	retryConfig    provider.RetryConfig
	circuitBreaker *provider.CircuitBreaker

	baseURL    string
	httpClient *http.Client

	mu            sync.RWMutex
	selectedModel model.Model
	metrics       provider.Metrics
}

func NewProvider(ctx context.Context, config Config, logger *slog.Logger) (*Provider, error) {
	if logger == nil {
		logger = slog.Default()
	}

	p := &Provider{
		config:         config,
		logger:         logger,
		retryConfig:    provider.DefaultRetryConfig(),
		circuitBreaker: provider.DefaultCircuitBreaker(logger.With("component", "circuit_breaker", "provider", "gemini")),
	}

	client, err := p.createClient(ctx)
	if err != nil {
		return nil, err
	}
	p.client = client

	return p, nil
}

// createClient builds a genai.Client from the provider's config and test fields
func (p *Provider) createClient(ctx context.Context) (*genai.Client, error) {
	var clientConfig *genai.ClientConfig

	if p.config.Vertex {
		clientConfig = &genai.ClientConfig{
			Project:  p.config.ProjectID,
			Location: p.config.Location,
			Backend:  genai.BackendVertexAI,
		}
	} else {
		apiKey := p.config.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("GOOGLE_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("GOOGLE_API_KEY or config.APIKey must be provided for AI Studio")
		}
		clientConfig = &genai.ClientConfig{
			APIKey:  apiKey,
			Backend: genai.BackendGeminiAPI,
		}
	}

	if p.baseURL != "" {
		clientConfig.HTTPOptions = genai.HTTPOptions{
			BaseURL: p.baseURL,
		}
	}

	if p.httpClient != nil {
		clientConfig.HTTPClient = p.httpClient
	}

	client, err := genai.NewClient(ctx, clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}
	return client, nil
}

// renewClient recreates the Gemini SDK client, re-reading credentials
// from the environment if the stored config values are stale
func (p *Provider) renewClient(ctx context.Context) error {
	p.mu.RLock()
	cfg := p.config
	p.mu.RUnlock()

	// Re-read API key from environment in case it was rotated
	if !cfg.Vertex && cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("GOOGLE_API_KEY")
	}

	// Temporarily apply the refreshed config so createClient uses it.
	p.mu.Lock()
	p.config = cfg
	p.mu.Unlock()

	client, err := p.createClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to renew gemini client: %w", err)
	}

	p.mu.Lock()
	p.client = client
	p.mu.Unlock()
	p.logger.Info("gemini client renewed")
	return nil
}

func (p *Provider) GetLogger() *slog.Logger {
	return p.logger
}

func (p *Provider) GetCircuitBreaker() *provider.CircuitBreaker {
	return p.circuitBreaker
}

func (p *Provider) GetName() string {
	return "Google Gemini"
}

func (p *Provider) GetID() string {
	return "gemini"
}

func (p *Provider) GetDescription() string {
	return "Google's most capable family of models for highly complex tasks."
}

func (p *Provider) GetHomepage() string {
	return "https://deepmind.google/models/gemini/"
}

func (p *Provider) GetMetrics() *provider.Metrics {
	return &p.metrics
}

// GetModels fetches available models from the Gemini API,
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
	for m, err := range client.Models.All(ctx) {
		if err != nil {
			return nil, fmt.Errorf("failed to fetch models: %w", err)
		}

		name := m.DisplayName
		if name == "" {
			name = m.Name
		}

		models = append(models, &Model{
			id:          m.Name,
			name:        name,
			description: m.Description,
			provider:    p,
			logger:      p.logger.With("model_id", m.Name),
		})
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

// Model implements the model.Model interface for a Google Gemini model
type Model struct {
	id          string
	name        string
	description string
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
	if m.description != "" {
		return m.description
	}
	return fmt.Sprintf("Gemini model: %s", m.id)
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
	return 0
}

func (m *Model) SupportsTools() bool {
	return true
}

func (m *Model) messageToParts(msg model.Message) []*genai.Part {
	var parts []*genai.Part
	for p := range msg.GetParts().Iterator() {
		switch v := p.GetContent().(type) {
		case string:
			parts = append(parts, genai.NewPartFromText(v))
		case model.ToolCall:
			var args map[string]any
			if err := json.Unmarshal([]byte(v.Arguments), &args); err == nil {
				parts = append(parts, &genai.Part{
					FunctionCall: &genai.FunctionCall{
						Name: v.Name,
						Args: args,
					},
				})
			}
		case model.ToolResult:
			response := make(map[string]any)
			if v.IsError {
				response["error"] = v.Content
			} else if err := json.Unmarshal([]byte(v.Content), &response); err != nil {
				// Tool results are often plain text (e.g. file contents). Wrap them
				// in the standard Gemini output envelope so the part is never dropped.
				response["output"] = v.Content
			}
			parts = append(parts, &genai.Part{
				FunctionResponse: &genai.FunctionResponse{
					Name:     v.Name,
					Response: response,
				},
			})
		default:
			m.GetLogger().Warn("unsupported message part content type", "type", reflect.TypeOf(v), "message_part", p)
		}
	}
	return parts
}

// Infer sends a streaming content generation request to the Gemini API,
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
	m.logger.Info("gemini: starting inference",
		"model", m.id,
		"history_messages", len(history),
		"tools", len(opts.Tools),
	)

	var contents []*genai.Content
	var systemParts []*genai.Part

	for _, msg := range history {
		if msg.GetRole() == model.MessageRoleSystem {
			systemParts = append(systemParts, m.messageToParts(msg)...)
			continue
		}

		role := "user"
		switch msg.GetRole() {
		case model.MessageRoleAssistant:
			role = "model"
		}

		contents = append(contents, &genai.Content{
			Parts: m.messageToParts(msg),
			Role:  role,
		})
	}

	var genConfig *genai.GenerateContentConfig
	if len(systemParts) > 0 {
		genConfig = &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: systemParts,
			},
		}
	} else {
		genConfig = &genai.GenerateContentConfig{}
	}

	// Apply Generation Config
	if opts.Config.MaxOutputTokens != nil {
		genConfig.MaxOutputTokens = int32(*opts.Config.MaxOutputTokens)
	}

	if opts.Config.Temperature != nil {
		v := float32(*opts.Config.Temperature)
		genConfig.Temperature = &v
	}
	if opts.Config.TopP != nil {
		v := float32(*opts.Config.TopP)
		genConfig.TopP = &v
	}
	if len(opts.Config.StopSequences) > 0 {
		genConfig.StopSequences = opts.Config.StopSequences
	}

	// Register Tools
	if len(opts.Tools) > 0 {
		functions := make([]*genai.FunctionDeclaration, 0, len(opts.Tools))
		for _, t := range opts.Tools {
			decl := &genai.FunctionDeclaration{
				Name:        t.GetName(),
				Description: t.GetDescription(),
			}
			if params := t.GetParameters(); params != nil {
				// Convert map to genai.Schema
				schemaData, _ := json.Marshal(params)
				var schema genai.Schema
				if err := json.Unmarshal(schemaData, &schema); err == nil {
					decl.Parameters = &schema
				}
			}
			functions = append(functions, decl)
		}
		genConfig.Tools = []*genai.Tool{{FunctionDeclarations: functions}}
	}

	resParts := structs.NewLinkedList[model.MessagePart]()
	if onPart == nil {
		onPart = func(p model.MessagePart) {
			m.logger.Debug("received message part", "part", p.GetContent())
		}
	}

	m.provider.mu.RLock()
	client := m.provider.client
	m.provider.mu.RUnlock()

	var usage model.Usage
	for resp, err := range client.Models.GenerateContentStream(ctx, m.id, contents, genConfig) {
		if err != nil {
			m.logger.Error("gemini: stream error", "model", m.id, "error", err)
			return nil, usage, fmt.Errorf("failed to generate content stream: %w", err)
		}

		if resp.UsageMetadata != nil {
			usage.PromptTokens = int(resp.UsageMetadata.PromptTokenCount)
			usage.CompletionTokens = int(resp.UsageMetadata.CandidatesTokenCount)
			usage.TotalTokens = int(resp.UsageMetadata.TotalTokenCount)
		}

		if len(resp.Candidates) > 0 && resp.Candidates[0].Content != nil {
			for _, p := range resp.Candidates[0].Content.Parts {
				if p.Text != "" {
					part := model.NewTextMessagePart(model.MessageRoleAssistant, p.Text)
					resParts.PushBack(part)
					onPart(part)
				}
				if p.FunctionCall != nil {
					args, _ := json.Marshal(p.FunctionCall.Args)
					tc := model.ToolCall{
						ID:        p.FunctionCall.ID,
						Name:      p.FunctionCall.Name,
						Arguments: string(args),
					}
					part := model.NewToolCallPart(tc)
					resParts.PushBack(part)
					onPart(part)
				}
			}
		}
	}

	if resParts.IsEmpty() {
		m.logger.Error("gemini: empty response — no text or function call parts received",
			"model", m.id,
			"prompt_tokens", usage.PromptTokens,
			"completion_tokens", usage.CompletionTokens,
		)
		return nil, usage, fmt.Errorf("gemini: empty response")
	}

	usage.Duration = time.Since(start)

	m.logger.Info("gemini: inference complete",
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
