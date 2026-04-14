package wizard

import (
	"os"
	"strings"
	"testing"

	"github.com/odinnordico/feino/internal/config"
	"github.com/odinnordico/feino/internal/i18n"
)

func TestWizardResult_ToConfig_Anthropic(t *testing.T) {
	r := WizardResult{
		Provider:     "anthropic",
		AnthropicKey: "sk-ant-testkey",
		DefaultModel: "claude-opus-4-6",
		WorkingDir:   "/workspace",
		Theme:        "dark",
	}
	cfg := r.ToConfig()
	if cfg.Providers.Anthropic.APIKey != "sk-ant-testkey" {
		t.Errorf("APIKey: got %q", cfg.Providers.Anthropic.APIKey)
	}
	if cfg.Providers.Anthropic.DefaultModel != "claude-opus-4-6" {
		t.Errorf("DefaultModel: got %q", cfg.Providers.Anthropic.DefaultModel)
	}
	if cfg.Security.PermissionLevel != "" {
		t.Errorf("PermissionLevel should not be set by wizard, got %q", cfg.Security.PermissionLevel)
	}
	if cfg.UI.Theme != "dark" {
		t.Errorf("Theme: got %q", cfg.UI.Theme)
	}
}

func TestWizardResult_ToConfig_OpenAI(t *testing.T) {
	r := WizardResult{
		Provider:      "openai",
		OpenAIKey:     "sk-oai-testkey",
		OpenAIBaseURL: "http://localhost:8080",
		DefaultModel:  "gpt-4o",
		Theme:         "light",
	}
	cfg := r.ToConfig()
	if cfg.Providers.OpenAI.APIKey != "sk-oai-testkey" {
		t.Errorf("APIKey: got %q", cfg.Providers.OpenAI.APIKey)
	}
	if cfg.Providers.OpenAI.BaseURL != "http://localhost:8080" {
		t.Errorf("BaseURL: got %q", cfg.Providers.OpenAI.BaseURL)
	}
	if cfg.Providers.OpenAI.DefaultModel != "gpt-4o" {
		t.Errorf("DefaultModel: got %q", cfg.Providers.OpenAI.DefaultModel)
	}
}

func TestWizardResult_ToConfig_Gemini(t *testing.T) {
	r := WizardResult{
		Provider:     "gemini",
		GeminiKey:    "gem-testkey",
		DefaultModel: "gemini-3.1-flash-lite-preview",
		Theme:        "auto",
	}
	cfg := r.ToConfig()
	if cfg.Providers.Gemini.APIKey != "gem-testkey" {
		t.Errorf("APIKey: got %q", cfg.Providers.Gemini.APIKey)
	}
	if cfg.Providers.Gemini.DefaultModel != "gemini-3.1-flash-lite-preview" {
		t.Errorf("DefaultModel: got %q", cfg.Providers.Gemini.DefaultModel)
	}
	if cfg.Providers.Gemini.Vertex == nil || *cfg.Providers.Gemini.Vertex {
		t.Error("Vertex: expected false pointer for API-key Gemini")
	}
}

func TestWizardResult_ToConfig_GeminiVertex(t *testing.T) {
	r := WizardResult{
		Provider:       "gemini",
		GeminiVertex:   true,
		VertexProject:  "my-project",
		VertexLocation: "us-central1",
		DefaultModel:   "gemini-pro",
		Theme:          "dark",
	}
	cfg := r.ToConfig()
	if cfg.Providers.Gemini.APIKey != "" {
		t.Errorf("APIKey: expected empty for Vertex path, got %q", cfg.Providers.Gemini.APIKey)
	}
	if cfg.Providers.Gemini.Vertex == nil || !*cfg.Providers.Gemini.Vertex {
		t.Error("Vertex: expected true")
	}
	if cfg.Providers.Gemini.ProjectID != "my-project" {
		t.Errorf("ProjectID: got %q", cfg.Providers.Gemini.ProjectID)
	}
	if cfg.Providers.Gemini.Location != "us-central1" {
		t.Errorf("Location: got %q", cfg.Providers.Gemini.Location)
	}
	if cfg.Providers.Gemini.DefaultModel != "gemini-pro" {
		t.Errorf("DefaultModel: got %q", cfg.Providers.Gemini.DefaultModel)
	}
}

func TestWizardResult_ToConfig_Ollama(t *testing.T) {
	r := WizardResult{
		Provider:     "ollama",
		OllamaHost:   "http://remote:11434",
		DefaultModel: "llama3",
		Theme:        "dark",
	}
	cfg := r.ToConfig()
	if cfg.Providers.Ollama.Host != "http://remote:11434" {
		t.Errorf("Host: got %q", cfg.Providers.Ollama.Host)
	}
	if cfg.Providers.Ollama.DefaultModel != "llama3" {
		t.Errorf("DefaultModel: got %q", cfg.Providers.Ollama.DefaultModel)
	}
}

func TestWizardResult_ToConfig_Ollama_DefaultHost(t *testing.T) {
	r := WizardResult{
		Provider:     "ollama",
		OllamaHost:   "",
		DefaultModel: "llama3",
	}
	cfg := r.ToConfig()
	if cfg.Providers.Ollama.Host != "http://localhost:11434" {
		t.Errorf("Host: got %q, want default", cfg.Providers.Ollama.Host)
	}
}

func TestWizardResult_ToConfig_OpenAICompat(t *testing.T) {
	r := WizardResult{
		Provider:            "openai_compat",
		OpenAICompatBaseURL: "http://localhost:8000/v1",
		OpenAICompatKey:     "my-token",
		OpenAICompatName:    "My vLLM",
		DefaultModel:        "mistral-7b",
		WorkingDir:          "/workspace",
		Theme:               "dark",
	}
	cfg := r.ToConfig()
	if cfg.Providers.OpenAICompat.BaseURL != "http://localhost:8000/v1" {
		t.Errorf("BaseURL: got %q", cfg.Providers.OpenAICompat.BaseURL)
	}
	if cfg.Providers.OpenAICompat.APIKey != "my-token" {
		t.Errorf("APIKey: got %q", cfg.Providers.OpenAICompat.APIKey)
	}
	if cfg.Providers.OpenAICompat.Name != "My vLLM" {
		t.Errorf("Name: got %q", cfg.Providers.OpenAICompat.Name)
	}
	if cfg.Providers.OpenAICompat.DefaultModel != "mistral-7b" {
		t.Errorf("DefaultModel: got %q", cfg.Providers.OpenAICompat.DefaultModel)
	}
	if cfg.UI.Theme != "dark" {
		t.Errorf("Theme: got %q", cfg.UI.Theme)
	}
}

func TestWizardResult_ToConfig_OpenAICompat_DefaultName(t *testing.T) {
	// When no name is given, ToConfig should default to the i18n provider label.
	r := WizardResult{
		Provider:            "openai_compat",
		OpenAICompatBaseURL: "http://localhost:8000/v1",
		DefaultModel:        "llama3",
	}
	cfg := r.ToConfig()
	want := i18n.T("provider_openai_compat")
	if cfg.Providers.OpenAICompat.Name != want {
		t.Errorf("Name: got %q, want %q", cfg.Providers.OpenAICompat.Name, want)
	}
}

func TestWizardResult_ToConfig_OpenAICompat_NoKey(t *testing.T) {
	// API key is optional — an empty key must be stored as-is (the provider
	// itself handles the "none" fallback).
	r := WizardResult{
		Provider:            "openai_compat",
		OpenAICompatBaseURL: "http://localhost:8000/v1",
		DefaultModel:        "qwen-2.5",
	}
	cfg := r.ToConfig()
	if cfg.Providers.OpenAICompat.APIKey != "" {
		t.Errorf("APIKey: expected empty, got %q", cfg.Providers.OpenAICompat.APIKey)
	}
}

// TestProviderPrefill_EnvVarFallback verifies that each provider's prefill reads
// env vars when the saved config has no credentials.
func TestGeminiVertexPrefill(t *testing.T) {
	var res WizardResult
	providers := buildProviders(&res)

	cfg := config.Config{
		Providers: config.ProvidersConfig{
			Gemini: config.GeminiConfig{
				Vertex:       new(true),
				ProjectID:    "gcp-proj",
				Location:     "us-east1",
				DefaultModel: "gemini-pro",
			},
		},
	}
	for _, p := range providers {
		if p.id == "gemini" && p.prefill != nil {
			p.prefill(cfg, &res)
			res.Provider = "gemini"
		}
	}

	if !res.GeminiVertex {
		t.Error("GeminiVertex: expected true")
	}
	if res.VertexProject != "gcp-proj" {
		t.Errorf("VertexProject: got %q", res.VertexProject)
	}
	if res.VertexLocation != "us-east1" {
		t.Errorf("VertexLocation: got %q", res.VertexLocation)
	}
	if res.GeminiKey != "" {
		t.Errorf("GeminiKey: expected empty on Vertex path, got %q", res.GeminiKey)
	}
}

func TestValidateURL(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"http://localhost:8000/v1", false},
		{"https://api.example.com", false},
		{"", true},
		{"   ", true},
		{"localhost:8000", true},
		{"ftp://wrong", true},
	}
	for _, tt := range tests {
		err := validateURL(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateURL(%q): err=%v, wantErr=%v", tt.input, err, tt.wantErr)
		}
	}
}

func TestRequireNonEmpty(t *testing.T) {
	fn := requireNonEmpty("API key")
	if err := fn(""); err == nil {
		t.Error("expected error for empty string")
	}
	if err := fn("   "); err == nil {
		t.Error("expected error for whitespace-only string")
	}
	if err := fn("sk-abc"); err != nil {
		t.Errorf("expected nil for non-empty string, got %v", err)
	}
	// Error message must mention the field name.
	if err := fn(""); !contains(err.Error(), "API key") {
		t.Errorf("error should mention field name, got %q", err.Error())
	}
}

func TestProviderPrefill_EnvVarFallback(t *testing.T) {
	tests := []struct {
		name    string
		envs    map[string]string
		wantID  string
		checkFn func(t *testing.T, res WizardResult)
	}{
		{
			name:   "anthropic from env",
			envs:   map[string]string{"ANTHROPIC_API_KEY": "sk-ant-env"},
			wantID: "anthropic",
			checkFn: func(t *testing.T, res WizardResult) {
				if res.AnthropicKey != "sk-ant-env" {
					t.Errorf("AnthropicKey: got %q, want sk-ant-env", res.AnthropicKey)
				}
			},
		},
		{
			name:   "openai key from env",
			envs:   map[string]string{"OPENAI_API_KEY": "sk-oai-env"},
			wantID: "openai",
			checkFn: func(t *testing.T, res WizardResult) {
				if res.OpenAIKey != "sk-oai-env" {
					t.Errorf("OpenAIKey: got %q, want sk-oai-env", res.OpenAIKey)
				}
				if res.OpenAIBaseURL != "" {
					t.Errorf("OpenAIBaseURL: expected empty when OPENAI_BASE_URL not set, got %q", res.OpenAIBaseURL)
				}
			},
		},
		{
			name:   "openai key and base url from env",
			envs:   map[string]string{"OPENAI_API_KEY": "sk-oai-env", "OPENAI_BASE_URL": "http://proxy:8080/v1"},
			wantID: "openai",
			checkFn: func(t *testing.T, res WizardResult) {
				if res.OpenAIKey != "sk-oai-env" {
					t.Errorf("OpenAIKey: got %q", res.OpenAIKey)
				}
				if res.OpenAIBaseURL != "http://proxy:8080/v1" {
					t.Errorf("OpenAIBaseURL: got %q", res.OpenAIBaseURL)
				}
			},
		},
		{
			name:   "gemini from env",
			envs:   map[string]string{"GEMINI_API_KEY": "gem-env"},
			wantID: "gemini",
			checkFn: func(t *testing.T, res WizardResult) {
				if res.GeminiKey != "gem-env" {
					t.Errorf("GeminiKey: got %q, want gem-env", res.GeminiKey)
				}
			},
		},
		{
			name:   "openai_compat base url from env",
			envs:   map[string]string{"OPENAI_COMPAT_BASE_URL": "http://gpu:8000/v1", "OPENAI_COMPAT_NAME": "GPU vLLM"},
			wantID: "openai_compat",
			checkFn: func(t *testing.T, res WizardResult) {
				if res.OpenAICompatBaseURL != "http://gpu:8000/v1" {
					t.Errorf("OpenAICompatBaseURL: got %q", res.OpenAICompatBaseURL)
				}
				if res.OpenAICompatName != "GPU vLLM" {
					t.Errorf("OpenAICompatName: got %q", res.OpenAICompatName)
				}
				if res.OpenAICompatKey != "" {
					t.Errorf("OpenAICompatKey: expected empty, got %q", res.OpenAICompatKey)
				}
			},
		},
		{
			name:   "openai_compat all fields from env",
			envs:   map[string]string{"OPENAI_COMPAT_BASE_URL": "http://gpu:8000/v1", "OPENAI_COMPAT_API_KEY": "tok-env", "OPENAI_COMPAT_NAME": "My GPU"},
			wantID: "openai_compat",
			checkFn: func(t *testing.T, res WizardResult) {
				if res.OpenAICompatKey != "tok-env" {
					t.Errorf("OpenAICompatKey: got %q", res.OpenAICompatKey)
				}
				if res.OpenAICompatName != "My GPU" {
					t.Errorf("OpenAICompatName: got %q", res.OpenAICompatName)
				}
			},
		},
		{
			name:    "no env vars — nothing prefilled",
			envs:    map[string]string{},
			wantID:  "",
			checkFn: func(t *testing.T, res WizardResult) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set test env vars and restore originals on cleanup.
			toUnset := []string{
				"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "OPENAI_BASE_URL",
				"GEMINI_API_KEY", "OLLAMA_HOST",
				"OPENAI_COMPAT_BASE_URL", "OPENAI_COMPAT_API_KEY", "OPENAI_COMPAT_NAME",
			}
			originals := make(map[string]string, len(toUnset))
			for _, k := range toUnset {
				originals[k] = os.Getenv(k)
				_ = os.Unsetenv(k)
			}
			t.Cleanup(func() {
				for k, v := range originals {
					if v == "" {
						_ = os.Unsetenv(k)
					} else {
						_ = os.Setenv(k, v)
					}
				}
			})
			for k, v := range tt.envs {
				_ = os.Setenv(k, v)
			}

			var res WizardResult
			providers := buildProviders(&res)

			for _, p := range providers {
				if p.prefill != nil && p.prefill(config.Config{}, &res) {
					if res.Provider == "" {
						res.Provider = p.id
					}
				}
			}

			if res.Provider != tt.wantID {
				t.Errorf("provider: got %q, want %q", res.Provider, tt.wantID)
			}
			tt.checkFn(t, res)
		})
	}
}

// TestProviderPrefill_ConfigWinsOverEnv verifies that a saved config value
// takes precedence over an environment variable (config was explicitly set by
// the user, env var might be a stale leftover).
func TestProviderPrefill_ConfigWinsOverEnv(t *testing.T) {
	_ = os.Setenv("ANTHROPIC_API_KEY", "sk-ant-env")
	t.Cleanup(func() { _ = os.Unsetenv("ANTHROPIC_API_KEY") })

	var res WizardResult
	providers := buildProviders(&res)

	cfg := config.Config{
		Providers: config.ProvidersConfig{
			Anthropic: config.AnthropicConfig{APIKey: "sk-ant-from-file"},
		},
	}
	for _, p := range providers {
		if p.id == "anthropic" && p.prefill != nil {
			p.prefill(cfg, &res)
		}
	}

	if res.AnthropicKey != "sk-ant-from-file" {
		t.Errorf("AnthropicKey: got %q, want config value sk-ant-from-file", res.AnthropicKey)
	}
}

func TestMaskKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"sk-ant-abc123xyz", "sk-a****"}, // long key: first 4 chars shown
		{"sk-oaiabcdefgh", "sk-o****"},
		{"gem-abcdefgh", "gem-****"},
		{"tok-abcdefgh", "tok-****"},
		{"short", "****"},        // < 8 chars: fully masked
		{"1234567", "****"},      // exactly 7 chars: fully masked
		{"12345678", "1234****"}, // exactly 8 chars: first 4 shown
		{"", "****"},
	}
	for _, tt := range tests {
		if got := maskKey(tt.input); got != tt.want {
			t.Errorf("maskKey(%q): got %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildSummary(t *testing.T) {
	var res WizardResult
	providers := buildProviders(&res)

	res = WizardResult{
		Provider:     "anthropic",
		AnthropicKey: "sk-ant-abc123",
		DefaultModel: "claude-opus-4-6",
		WorkingDir:   "/home/user",
		Theme:        "dark",
	}
	got := buildSummary(res, providers)
	// Should show label ("Anthropic (Claude)"), not raw ID ("anthropic").
	if contains(got, "Provider:    anthropic\n") {
		t.Errorf("summary should show provider label, not raw ID:\n%s", got)
	}
	if want := "Anthropic (Claude)"; !contains(got, want) {
		t.Errorf("summary missing provider label %q:\n%s", want, got)
	}
	if want := "sk-a****"; !contains(got, want) {
		t.Errorf("summary missing masked key %q:\n%s", want, got)
	}
	if want := "claude-opus-4-6"; !contains(got, want) {
		t.Errorf("summary missing model %q:\n%s", want, got)
	}

	// Unknown provider should not panic.
	res.Provider = "unknown"
	_ = buildSummary(res, providers)
}

func TestProviderPrefill(t *testing.T) {
	tests := []struct {
		name       string
		cfg        config.Config
		wantID     string
		wantFields func(res WizardResult) bool
	}{
		{
			name: "anthropic prefills key and model",
			cfg: config.Config{
				Providers: config.ProvidersConfig{
					Anthropic: config.AnthropicConfig{APIKey: "sk-ant-abc", DefaultModel: "claude-opus-4-6"},
				},
			},
			wantID: "anthropic",
			wantFields: func(r WizardResult) bool {
				return r.AnthropicKey == "sk-ant-abc" && r.DefaultModel == "claude-opus-4-6"
			},
		},
		{
			name: "openai prefills key, base url, and model",
			cfg: config.Config{
				Providers: config.ProvidersConfig{
					OpenAI: config.OpenAIConfig{APIKey: "sk-oai", BaseURL: "http://proxy", DefaultModel: "gpt-4o"},
				},
			},
			wantID: "openai",
			wantFields: func(r WizardResult) bool {
				return r.OpenAIKey == "sk-oai" && r.OpenAIBaseURL == "http://proxy" && r.DefaultModel == "gpt-4o"
			},
		},
		{
			name: "gemini prefills key and model",
			cfg: config.Config{
				Providers: config.ProvidersConfig{
					Gemini: config.GeminiConfig{APIKey: "gem-key", DefaultModel: "gemini-pro"},
				},
			},
			wantID: "gemini",
			wantFields: func(r WizardResult) bool {
				return r.GeminiKey == "gem-key" && r.DefaultModel == "gemini-pro"
			},
		},
		{
			name: "ollama",
			cfg: config.Config{
				Providers: config.ProvidersConfig{
					Ollama: config.OllamaConfig{DefaultModel: "llama3", Host: "http://remote:11434"},
				},
			},
			wantID:     "ollama",
			wantFields: func(r WizardResult) bool { return r.OllamaHost == "http://remote:11434" },
		},
		{
			name: "openai_compat prefills base url, key, and name",
			cfg: config.Config{
				Providers: config.ProvidersConfig{
					OpenAICompat: config.OpenAICompatConfig{
						BaseURL:      "http://gpu-server:8000/v1",
						APIKey:       "tok-abc",
						Name:         "GPU vLLM",
						DefaultModel: "mistral-7b",
					},
				},
			},
			wantID: "openai_compat",
			wantFields: func(r WizardResult) bool {
				return r.OpenAICompatBaseURL == "http://gpu-server:8000/v1" &&
					r.OpenAICompatKey == "tok-abc" &&
					r.OpenAICompatName == "GPU vLLM" &&
					r.DefaultModel == "mistral-7b"
			},
		},
		{
			name:   "empty config prefills nothing",
			cfg:    config.Config{},
			wantID: "",
			wantFields: func(r WizardResult) bool {
				return r.AnthropicKey == "" && r.OpenAIKey == "" && r.GeminiKey == "" &&
					r.OllamaHost == "" && r.OpenAICompatBaseURL == ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var res WizardResult
			providers := buildProviders(&res)

			for _, p := range providers {
				if p.prefill != nil && p.prefill(tt.cfg, &res) {
					if res.Provider == "" {
						res.Provider = p.id
					}
				}
			}

			if res.Provider != tt.wantID {
				t.Errorf("provider: got %q, want %q", res.Provider, tt.wantID)
			}
			if !tt.wantFields(res) {
				t.Errorf("unexpected field values after prefill: %+v", res)
			}
		})
	}
}

func TestProviderSummary(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(res *WizardResult)
		provider string
		contains string
	}{
		{
			name:     "anthropic masks key",
			setup:    func(r *WizardResult) { r.AnthropicKey = "sk-ant-abc123xyz" },
			provider: "anthropic",
			contains: "sk-a****",
		},
		{
			name:     "openai masks key",
			setup:    func(r *WizardResult) { r.OpenAIKey = "sk-oaiabcdefgh" },
			provider: "openai",
			contains: "sk-o****",
		},
		{
			name:     "openai includes base url",
			setup:    func(r *WizardResult) { r.OpenAIKey = "sk-x"; r.OpenAIBaseURL = "http://proxy" },
			provider: "openai",
			contains: "http://proxy",
		},
		{
			name:     "gemini masks key",
			setup:    func(r *WizardResult) { r.GeminiKey = "gem-abcdefgh" },
			provider: "gemini",
			contains: "gem-****",
		},
		{
			name: "gemini vertex shows project and location",
			setup: func(r *WizardResult) {
				r.GeminiVertex = true
				r.VertexProject = "my-project"
				r.VertexLocation = "us-central1"
			},
			provider: "gemini",
			contains: "Vertex AI",
		},
		{
			name: "gemini vertex summary includes project",
			setup: func(r *WizardResult) {
				r.GeminiVertex = true
				r.VertexProject = "my-project"
				r.VertexLocation = "eu-west1"
			},
			provider: "gemini",
			contains: "my-project",
		},
		{
			name:     "ollama default host",
			setup:    func(r *WizardResult) { r.OllamaHost = "" },
			provider: "ollama",
			contains: "localhost:11434",
		},
		{
			name:     "ollama custom host",
			setup:    func(r *WizardResult) { r.OllamaHost = "http://remote:11434" },
			provider: "ollama",
			contains: "http://remote:11434",
		},
		{
			name:     "openai_compat shows base url",
			setup:    func(r *WizardResult) { r.OpenAICompatBaseURL = "http://gpu:8000/v1" },
			provider: "openai_compat",
			contains: "http://gpu:8000/v1",
		},
		{
			name: "openai_compat masks key when present",
			setup: func(r *WizardResult) {
				r.OpenAICompatBaseURL = "http://gpu:8000/v1"
				r.OpenAICompatKey = "tok-abcdefgh"
			},
			provider: "openai_compat",
			contains: "tok-****",
		},
		{
			name: "openai_compat shows name",
			setup: func(r *WizardResult) {
				r.OpenAICompatBaseURL = "http://gpu:8000/v1"
				r.OpenAICompatName = "My GPU"
			},
			provider: "openai_compat",
			contains: "My GPU",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var res WizardResult
			providers := buildProviders(&res)
			tt.setup(&res)

			for _, p := range providers {
				if p.id == tt.provider {
					got := p.summary()
					if !contains(got, tt.contains) {
						t.Errorf("summary = %q, want it to contain %q", got, tt.contains)
					}
					return
				}
			}
			t.Errorf("provider %q not found", tt.provider)
		})
	}
}

func TestValidatePort(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"80", false},
		{"1", false},
		{"65535", false},
		{"443", false},
		{"0", true},
		{"65536", true},
		{"-1", true},
		{"abc", true},
		{"", true},
		{"   ", true},
		{"  443  ", false}, // leading/trailing whitespace is trimmed
	}
	for _, tt := range tests {
		err := validatePort(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("validatePort(%q): err=%v, wantErr=%v", tt.input, err, tt.wantErr)
		}
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"user@example.com", false},
		{"a@b", false},
		{"", true},
		{"   ", true},
		{"notanemail", true},
	}
	for _, tt := range tests {
		err := validateEmail(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateEmail(%q): err=%v, wantErr=%v", tt.input, err, tt.wantErr)
		}
	}
}

func TestDefaultTimezone(t *testing.T) {
	tz := defaultTimezone()
	if tz == "" {
		t.Error("defaultTimezone: must not return empty string")
	}
	if tz == "Local" {
		t.Error("defaultTimezone: must not return \"Local\"")
	}
}

func TestDefaultWorkingDir(t *testing.T) {
	wd := defaultWorkingDir()
	if wd == "" {
		t.Error("defaultWorkingDir: must not return empty string in a normal test environment")
	}
}

func TestSaveEmailSetup(t *testing.T) {
	store := &fakeCredStore{data: map[string]string{}}
	var cfg config.Config

	res := EmailSetupResult{
		Address:  "user@example.com",
		Username: "user@example.com",
		Password: "secret",
		IMAPHost: "imap.example.com",
		IMAPPort: 993,
		SMTPHost: "smtp.example.com",
		SMTPPort: 587,
	}

	if err := SaveEmailSetup(res, &cfg, store); err != nil {
		t.Fatalf("SaveEmailSetup: unexpected error: %v", err)
	}

	if cfg.Services.Email.Address != "user@example.com" {
		t.Errorf("Address: got %q", cfg.Services.Email.Address)
	}
	if cfg.Services.Email.IMAPHost != "imap.example.com" {
		t.Errorf("IMAPHost: got %q", cfg.Services.Email.IMAPHost)
	}
	if cfg.Services.Email.IMAPPort != 993 {
		t.Errorf("IMAPPort: got %d", cfg.Services.Email.IMAPPort)
	}
	if cfg.Services.Email.SMTPHost != "smtp.example.com" {
		t.Errorf("SMTPHost: got %q", cfg.Services.Email.SMTPHost)
	}
	if cfg.Services.Email.SMTPPort != 587 {
		t.Errorf("SMTPPort: got %d", cfg.Services.Email.SMTPPort)
	}
	if cfg.Services.Email.Enabled == nil || !*cfg.Services.Email.Enabled {
		t.Error("Enabled: expected true pointer")
	}

	if store.data["email/username"] != "user@example.com" {
		t.Errorf("credential username: got %q", store.data["email/username"])
	}
	if store.data["email/password"] != "secret" {
		t.Errorf("credential password: got %q", store.data["email/password"])
	}
}

// fakeCredStore implements credentials.Store for testing.
type fakeCredStore struct {
	data map[string]string
}

func (f *fakeCredStore) Set(service, key, value string) error {
	f.data[service+"/"+key] = value
	return nil
}
func (f *fakeCredStore) Get(service, key string) (string, error) {
	return f.data[service+"/"+key], nil
}
func (f *fakeCredStore) Delete(service, key string) error {
	delete(f.data, service+"/"+key)
	return nil
}
func (f *fakeCredStore) Clear(service string) error {
	prefix := service + "/"
	for k := range f.data {
		if strings.HasPrefix(k, prefix) {
			delete(f.data, k)
		}
	}
	return nil
}
func (f *fakeCredStore) List(service string) ([]string, error) {
	prefix := service + "/"
	var keys []string
	for k := range f.data {
		if after, ok := strings.CutPrefix(k, prefix); ok {
			keys = append(keys, after)
		}
	}
	return keys, nil
}
func (f *fakeCredStore) Kind() string { return "fake" }

func contains(s, substr string) bool { return strings.Contains(s, substr) }
