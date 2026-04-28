package wizard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"github.com/odinnordico/feino/internal/config"
	"github.com/odinnordico/feino/internal/i18n"
)

// defaultOllamaHost is the canonical fallback used when no Ollama host is
// configured. Defined as a constant to keep the three usage sites in sync:
// fetchOllamaModels, the summary closure, and WizardResult.ToConfig.
const defaultOllamaHost = "http://localhost:11434"

// wizardProvider describes a single LLM provider's wizard contribution.
//
// To add a new provider:
//  1. Write a constructor function following the pattern below.
//  2. Append it to the slice returned by buildProviders — nothing else changes.
type wizardProvider struct {
	id    string
	label string

	// credGroups are the huh groups for this provider's credential step(s).
	// Each group must already have its WithHideFunc set so it is only visible
	// when res.Provider matches this provider's id.
	credGroups []*huh.Group

	// modelGroup is an optional provider-specific model-selection step.
	// When non-nil it is inserted before the shared model input, and the
	// shared input is hidden for this provider. The group must have its own
	// WithHideFunc set to only show when res.Provider == this provider's id.
	modelGroup *huh.Group

	// prefill copies any existing credentials from cfg into res and returns
	// true when credentials were found. The first provider that returns true
	// becomes the pre-selected provider in the form.
	prefill func(cfg config.Config, res *Result) bool

	// summary returns the credential portion of the confirmation summary.
	// It is called after form.Run() returns, so res reflects the user's entries.
	summary func() string

	// finalize sets any derived fields on res after form.Run() returns.
	// May be nil when no post-processing is needed.
	finalize func(res *Result)
}

// buildProviders constructs all registered provider definitions.
// res is the shared WizardResult pointer whose fields the form will populate.
func buildProviders(res *Result) []*wizardProvider {
	return []*wizardProvider{
		anthropicProvider(res),
		openaiProvider(res),
		geminiProvider(res),
		ollamaProvider(res),
		openaiCompatProvider(res),
	}
}

func anthropicProvider(res *Result) *wizardProvider {
	return &wizardProvider{
		id:    "anthropic",
		label: i18n.T("provider_anthropic"),
		credGroups: []*huh.Group{
			huh.NewGroup(
				huh.NewInput().
					Title(i18n.T("anthropic_key_title")).
					Description(i18n.T("anthropic_key_desc")).
					EchoMode(huh.EchoModePassword).
					Validate(requireNonEmpty(i18n.T("summary_api_key"))).
					Value(&res.AnthropicKey),
			).WithHideFunc(func() bool { return res.Provider != "anthropic" }),
		},
		modelGroup: huh.NewGroup(
			huh.NewSelect[string]().
				Title(i18n.T("model_select_title")).
				Description(i18n.T("anthropic_model_desc")).
				OptionsFunc(func() []huh.Option[string] {
					return fetchAnthropicModels(res.AnthropicKey)
				}, &res.AnthropicKey).
				Validate(requireNonEmpty(i18n.T("summary_model"))).
				Value(&res.DefaultModel),
		).WithHideFunc(func() bool { return res.Provider != "anthropic" }),
		prefill: func(cfg config.Config, res *Result) bool {
			key := cfg.Providers.Anthropic.APIKey
			if key == "" {
				key = os.Getenv("ANTHROPIC_API_KEY")
			}
			if key == "" {
				return false
			}
			res.AnthropicKey = key
			res.DefaultModel = cfg.Providers.Anthropic.DefaultModel
			return true
		},
		summary: func() string {
			return i18n.T("summary_api_key") + ": " + maskKey(res.AnthropicKey)
		},
	}
}

func openaiProvider(res *Result) *wizardProvider {
	return &wizardProvider{
		id:    "openai",
		label: i18n.T("provider_openai"),
		credGroups: []*huh.Group{
			huh.NewGroup(
				huh.NewInput().
					Title(i18n.T("openai_key_title")).
					Description(i18n.T("openai_key_desc")).
					EchoMode(huh.EchoModePassword).
					Validate(requireNonEmpty(i18n.T("summary_api_key"))).
					Value(&res.OpenAIKey),
				huh.NewInput().
					Title(i18n.T("openai_baseurl_title")).
					Description(i18n.T("openai_baseurl_desc")).
					Placeholder("https://api.openai.com/v1").
					Validate(func(s string) error {
						if s == "" {
							return nil // field is optional
						}
						return validateURL(s)
					}).
					Value(&res.OpenAIBaseURL),
			).WithHideFunc(func() bool { return res.Provider != "openai" }),
		},
		modelGroup: huh.NewGroup(
			huh.NewSelect[string]().
				Title(i18n.T("model_select_title")).
				Description(i18n.T("openai_model_desc")).
				OptionsFunc(func() []huh.Option[string] {
					return fetchOpenAIModels(res.OpenAIKey, res.OpenAIBaseURL)
				}, &res.OpenAIKey).
				Validate(requireNonEmpty(i18n.T("summary_model"))).
				Value(&res.DefaultModel),
		).WithHideFunc(func() bool { return res.Provider != "openai" }),
		prefill: func(cfg config.Config, res *Result) bool {
			key := cfg.Providers.OpenAI.APIKey
			if key == "" {
				key = os.Getenv("OPENAI_API_KEY")
			}
			if key == "" {
				return false
			}
			res.OpenAIKey = key
			res.OpenAIBaseURL = cfg.Providers.OpenAI.BaseURL
			if res.OpenAIBaseURL == "" {
				res.OpenAIBaseURL = os.Getenv("OPENAI_BASE_URL")
			}
			res.DefaultModel = cfg.Providers.OpenAI.DefaultModel
			return true
		},
		summary: func() string {
			s := i18n.T("summary_api_key") + ": " + maskKey(res.OpenAIKey)
			if res.OpenAIBaseURL != "" {
				s += "\n" + i18n.T("summary_base_url") + ": " + res.OpenAIBaseURL
			}
			return s
		},
	}
}

func geminiProvider(res *Result) *wizardProvider {
	return &wizardProvider{
		id:    "gemini",
		label: i18n.T("provider_gemini"),
		credGroups: []*huh.Group{
			// Step 1 (always shown for gemini): choose auth method.
			huh.NewGroup(
				huh.NewConfirm().
					Title(i18n.T("gemini_vertex_title")).
					Description(i18n.T("gemini_vertex_desc")).
					Value(&res.GeminiVertex),
			).WithHideFunc(func() bool { return res.Provider != "gemini" }),

			// Step 2a: API key path (shown when NOT using Vertex).
			huh.NewGroup(
				huh.NewInput().
					Title(i18n.T("gemini_key_title")).
					Description(i18n.T("gemini_key_desc")).
					EchoMode(huh.EchoModePassword).
					Validate(func(s string) error {
						if res.GeminiVertex {
							return nil // not required on this path
						}
						return requireNonEmpty(i18n.T("summary_api_key"))(s)
					}).
					Value(&res.GeminiKey),
			).WithHideFunc(func() bool { return res.Provider != "gemini" || res.GeminiVertex }),

			// Step 2b: Vertex credentials (shown only when Vertex is selected).
			huh.NewGroup(
				huh.NewInput().
					Title(i18n.T("gemini_project_title")).
					Description(i18n.T("gemini_project_desc")).
					Validate(func(s string) error {
						if !res.GeminiVertex {
							return nil
						}
						return requireNonEmpty(i18n.T("summary_project"))(s)
					}).
					Value(&res.VertexProject),
				huh.NewInput().
					Title(i18n.T("gemini_location_title")).
					Description(i18n.T("gemini_location_desc")).
					Placeholder("us-central1").
					Validate(func(s string) error {
						if !res.GeminiVertex {
							return nil
						}
						return requireNonEmpty(i18n.T("summary_location"))(s)
					}).
					Value(&res.VertexLocation),
			).WithHideFunc(func() bool { return res.Provider != "gemini" || !res.GeminiVertex }),
		},
		modelGroup: huh.NewGroup(
			huh.NewSelect[string]().
				Title(i18n.T("model_select_title")).
				Description(i18n.T("gemini_model_desc")).
				OptionsFunc(func() []huh.Option[string] {
					return fetchGeminiModels(res.GeminiKey)
				}, &res.GeminiKey).
				Validate(requireNonEmpty(i18n.T("summary_model"))).
				Value(&res.DefaultModel),
		).WithHideFunc(func() bool { return res.Provider != "gemini" }),
		prefill: func(cfg config.Config, res *Result) bool {
			// Vertex AI path.
			if cfg.Providers.Gemini.Vertex != nil && *cfg.Providers.Gemini.Vertex {
				res.GeminiVertex = true
				res.VertexProject = cfg.Providers.Gemini.ProjectID
				res.VertexLocation = cfg.Providers.Gemini.Location
				res.DefaultModel = cfg.Providers.Gemini.DefaultModel
				return true
			}
			// API key path.
			key := cfg.Providers.Gemini.APIKey
			if key == "" {
				key = os.Getenv("GEMINI_API_KEY")
			}
			if key == "" {
				return false
			}
			res.GeminiKey = key
			res.DefaultModel = cfg.Providers.Gemini.DefaultModel
			return true
		},
		summary: func() string {
			if res.GeminiVertex {
				return fmt.Sprintf("%s: %s\n%s: %s\n%s: %s",
					i18n.T("summary_auth"), i18n.T("summary_vertex_auth"),
					i18n.T("summary_project"), res.VertexProject,
					i18n.T("summary_location"), res.VertexLocation)
			}
			return i18n.T("summary_api_key") + ": " + maskKey(res.GeminiKey)
		},
	}
}

func ollamaProvider(res *Result) *wizardProvider {
	// Try to list locally-available models before the form runs so we can
	// offer a select instead of a text input. The host may already be
	// known if this is a re-run via /setup; otherwise we probe localhost.
	modelNames := fetchOllamaModels(res.OllamaHost)

	var modelGroup *huh.Group
	if len(modelNames) > 0 {
		opts := make([]huh.Option[string], len(modelNames))
		for i, name := range modelNames {
			opts[i] = huh.NewOption(name, name)
		}
		modelGroup = huh.NewGroup(
			huh.NewSelect[string]().
				Title(i18n.T("model_select_title")).
				Description(i18n.T("ollama_model_select_desc")).
				Options(opts...).
				Value(&res.DefaultModel),
		).WithHideFunc(func() bool { return res.Provider != "ollama" })
	} else {
		modelGroup = huh.NewGroup(
			huh.NewInput().
				Title(i18n.T("model_select_title")).
				Description(i18n.T("ollama_model_input_desc")).
				Validate(requireNonEmpty(i18n.T("summary_model"))).
				Value(&res.DefaultModel),
		).WithHideFunc(func() bool { return res.Provider != "ollama" })
	}

	return &wizardProvider{
		id:         "ollama",
		label:      i18n.T("provider_ollama"),
		modelGroup: modelGroup,
		credGroups: []*huh.Group{
			huh.NewGroup(
				huh.NewInput().
					Title(i18n.T("ollama_host_title")).
					Description(i18n.T("ollama_host_desc")).
					Placeholder(defaultOllamaHost).
					Value(&res.OllamaHost),
			).WithHideFunc(func() bool { return res.Provider != "ollama" }),
		},
		prefill: func(cfg config.Config, res *Result) bool {
			if cfg.Providers.Ollama.DefaultModel == "" {
				return false
			}
			res.OllamaHost = cfg.Providers.Ollama.Host
			if res.OllamaHost == "" {
				res.OllamaHost = os.Getenv("OLLAMA_HOST")
			}
			res.DefaultModel = cfg.Providers.Ollama.DefaultModel
			return true
		},
		summary: func() string {
			host := res.OllamaHost
			if host == "" {
				host = defaultOllamaHost
			}
			return i18n.T("summary_host") + ": " + host
		},
	}
}

func openaiCompatProvider(res *Result) *wizardProvider {
	return &wizardProvider{
		id:    "openai_compat",
		label: i18n.T("provider_openai_compat"),
		credGroups: []*huh.Group{
			huh.NewGroup(
				huh.NewInput().
					Title(i18n.T("compat_baseurl_title")).
					Description(i18n.T("compat_baseurl_desc")).
					Placeholder("http://localhost:8000/v1").
					Validate(validateURL).
					Value(&res.OpenAICompatBaseURL),
				huh.NewInput().
					Title(i18n.T("compat_key_title")).
					Description(i18n.T("compat_key_desc")).
					EchoMode(huh.EchoModePassword).
					Value(&res.OpenAICompatKey),
				huh.NewInput().
					Title(i18n.T("compat_name_title")).
					Description(i18n.T("compat_name_desc")).
					Placeholder("OpenAI-Compatible").
					Value(&res.OpenAICompatName),
			).WithHideFunc(func() bool { return res.Provider != "openai_compat" }),
		},
		modelGroup: huh.NewGroup(
			huh.NewSelect[string]().
				Title(i18n.T("model_select_title")).
				Description(i18n.T("compat_model_desc")).
				OptionsFunc(func() []huh.Option[string] {
					return fetchOpenAIModels(res.OpenAICompatKey, res.OpenAICompatBaseURL)
				}, &res.OpenAICompatBaseURL).
				Validate(requireNonEmpty(i18n.T("summary_model"))).
				Value(&res.DefaultModel),
		).WithHideFunc(func() bool { return res.Provider != "openai_compat" }),
		prefill: func(cfg config.Config, res *Result) bool {
			baseURL := cfg.Providers.OpenAICompat.BaseURL
			if baseURL == "" {
				baseURL = os.Getenv("OPENAI_COMPAT_BASE_URL")
			}
			if baseURL == "" {
				return false
			}
			res.OpenAICompatBaseURL = baseURL
			res.OpenAICompatKey = cfg.Providers.OpenAICompat.APIKey
			if res.OpenAICompatKey == "" {
				res.OpenAICompatKey = os.Getenv("OPENAI_COMPAT_API_KEY")
			}
			res.OpenAICompatName = cfg.Providers.OpenAICompat.Name
			if res.OpenAICompatName == "" {
				res.OpenAICompatName = os.Getenv("OPENAI_COMPAT_NAME")
			}
			res.DefaultModel = cfg.Providers.OpenAICompat.DefaultModel
			return true
		},
		summary: func() string {
			s := i18n.T("summary_base_url") + ": " + res.OpenAICompatBaseURL
			if res.OpenAICompatKey != "" {
				s += "\n" + i18n.T("summary_api_key") + ": " + maskKey(res.OpenAICompatKey)
			}
			name := res.OpenAICompatName
			if name == "" {
				name = i18n.T("provider_openai_compat")
			}
			s += "\n" + i18n.T("summary_name") + ": " + name
			return s
		},
		finalize: func(res *Result) {
			if res.OpenAICompatName == "" {
				res.OpenAICompatName = i18n.T("provider_openai_compat")
			}
		},
	}
}

// doGetJSON executes req, checks for HTTP 200, and decodes the JSON body into
// target. Returns a non-nil error on any network, status, or decode failure.
func doGetJSON(req *http.Request, target any) error {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("network error: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned %d — check your key", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return errors.New("could not parse response")
	}
	return nil
}

// fetchAnthropicModels queries the Anthropic API for available models.
// Returns nil on any error (caller shows an error option).
func fetchAnthropicModels(apiKey string) []huh.Option[string] {
	if strings.TrimSpace(apiKey) == "" {
		return unavailableOption("API key is empty")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.anthropic.com/v1/models", nil)
	if err != nil {
		return unavailableOption("could not build request")
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	var result struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := doGetJSON(req, &result); err != nil {
		return unavailableOption(err.Error())
	}
	if len(result.Data) == 0 {
		return unavailableOption("no models returned")
	}

	opts := make([]huh.Option[string], len(result.Data))
	for i, m := range result.Data {
		label := m.DisplayName
		if label == "" {
			label = m.ID
		}
		opts[i] = huh.NewOption(label, m.ID)
	}
	return opts
}

// fetchOpenAIModels queries an OpenAI-compatible API for available models.
// baseURL defaults to https://api.openai.com/v1 when empty.
// apiKey is optional: when empty the Authorization header is omitted so that
// servers running without authentication (vLLM, LocalAI, LM Studio, …) are
// still queried. An error is returned only when both apiKey and baseURL are
// empty, because in that case there is nothing to connect to.
func fetchOpenAIModels(apiKey, baseURL string) []huh.Option[string] {
	if strings.TrimSpace(apiKey) == "" && strings.TrimSpace(baseURL) == "" {
		return unavailableOption("API key is empty")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.openai.com/v1"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return unavailableOption("could not build request")
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := doGetJSON(req, &result); err != nil {
		return unavailableOption(err.Error())
	}
	if len(result.Data) == 0 {
		return unavailableOption("no models returned")
	}

	opts := make([]huh.Option[string], len(result.Data))
	for i, m := range result.Data {
		opts[i] = huh.NewOption(m.ID, m.ID)
	}
	return opts
}

// fetchGeminiModels queries the Gemini API for available models.
func fetchGeminiModels(apiKey string) []huh.Option[string] {
	if strings.TrimSpace(apiKey) == "" {
		return unavailableOption("API key is empty")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://generativelanguage.googleapis.com/v1beta/models?key="+apiKey, nil)
	if err != nil {
		return unavailableOption("could not build request")
	}

	var result struct {
		Models []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"models"`
	}
	if err := doGetJSON(req, &result); err != nil {
		return unavailableOption(err.Error())
	}
	if len(result.Models) == 0 {
		return unavailableOption("no models returned")
	}

	opts := make([]huh.Option[string], len(result.Models))
	for i, m := range result.Models {
		label := m.DisplayName
		if label == "" {
			label = m.Name
		}
		opts[i] = huh.NewOption(label, m.Name)
	}
	return opts
}

// fetchOllamaModels queries the Ollama REST API for locally-available models.
// It uses host if non-empty, otherwise falls back to http://localhost:11434.
// Returns nil on any error (caller treats nil as "fall back to text input").
func fetchOllamaModels(host string) []string {
	if host == "" {
		host = defaultOllamaHost
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, host+"/api/tags", nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}

	names := make([]string, len(result.Models))
	for i, m := range result.Models {
		names[i] = m.Name
	}
	return names
}

// unavailableOption returns a single select option explaining why models
// could not be fetched. The empty value fails validation, so the user
// must abort (Ctrl+C) and correct their credentials before retrying.
func unavailableOption(reason string) []huh.Option[string] {
	return []huh.Option[string]{
		huh.NewOption(i18n.Tf("models_unavailable", map[string]any{"Reason": reason}), ""),
	}
}

// maskKey returns the first 4 characters of key followed by "****".
// Keys shorter than 8 characters are fully masked to avoid leaking short tokens.
func maskKey(key string) string {
	if len(key) < 8 {
		return "****"
	}
	return key[:4] + "****"
}

// requireNonEmpty returns a huh validation function that rejects blank input.
// field is the human-readable field name used in the error message.
func requireNonEmpty(field string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New(i18n.Tf("validation_required", map[string]any{"Field": field}))
		}
		return nil
	}
}

// validateURL returns an error if s is blank or does not start with http:// or https://.
func validateURL(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New(i18n.T("validation_url_required"))
	}
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return errors.New(i18n.T("validation_url_format"))
	}
	return nil
}
