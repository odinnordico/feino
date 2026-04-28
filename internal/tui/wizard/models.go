// Package wizard provides an interactive first-run setup flow built on
// charmbracelet/huh. It collects provider credentials, model preferences,
// security settings, and UI theme choices, then persists them to config.
package wizard

import (
	"errors"
	"os"
	"time"

	"github.com/odinnordico/feino/internal/config"
	"github.com/odinnordico/feino/internal/i18n"
)

// ErrAborted is returned when the user exits the wizard before completing it.
var ErrAborted = errors.New("wizard: setup aborted by user")

// Result holds all values collected by the wizard. Convert to a
// config.Config via ToConfig.
type Result struct {
	// Provider is one of "anthropic", "openai", "gemini", "ollama", "openai_compat".
	Provider string

	// Provider credentials.
	AnthropicKey   string
	OpenAIKey      string
	OpenAIBaseURL  string
	GeminiKey      string
	GeminiVertex   bool
	VertexProject  string
	VertexLocation string
	OllamaHost     string

	// OpenAI-compatible (vLLM, LocalAI, …) credentials.
	OpenAICompatBaseURL string
	OpenAICompatKey     string
	OpenAICompatName    string

	// Model.
	DefaultModel string

	// Context.
	WorkingDir string

	// UI.
	Theme string // dark | light | auto

	// User profile.
	Name               string // user's preferred name
	Timezone           string // IANA timezone, e.g. "America/Bogota"
	CommunicationStyle string // concise | detailed | technical | friendly
}

// ToConfig converts a WizardResult into a config.Config ready for config.Save.
func (r Result) ToConfig() config.Config {
	cfg := config.Config{
		Context: config.ContextConfig{
			WorkingDir: r.WorkingDir,
		},
		UI: config.UIConfig{
			Theme: r.Theme,
		},
		User: config.UserProfileConfig{
			Name:               r.Name,
			Timezone:           r.Timezone,
			CommunicationStyle: r.CommunicationStyle,
		},
	}

	switch r.Provider {
	case "anthropic":
		cfg.Providers.Anthropic = config.AnthropicConfig{
			APIKey:       r.AnthropicKey,
			DefaultModel: r.DefaultModel,
		}
	case "openai":
		cfg.Providers.OpenAI = config.OpenAIConfig{
			APIKey:       r.OpenAIKey,
			BaseURL:      r.OpenAIBaseURL,
			DefaultModel: r.DefaultModel,
		}
	case "gemini":
		if r.GeminiVertex {
			cfg.Providers.Gemini = config.GeminiConfig{
				DefaultModel: r.DefaultModel,
				Vertex:       new(true),
				ProjectID:    r.VertexProject,
				Location:     r.VertexLocation,
			}
		} else {
			cfg.Providers.Gemini = config.GeminiConfig{
				APIKey:       r.GeminiKey,
				DefaultModel: r.DefaultModel,
				Vertex:       new(false),
			}
		}
	case "ollama":
		host := r.OllamaHost
		if host == "" {
			host = defaultOllamaHost
		}
		cfg.Providers.Ollama = config.OllamaConfig{
			Host:         host,
			DefaultModel: r.DefaultModel,
		}
	case "openai_compat":
		name := r.OpenAICompatName
		if name == "" {
			name = i18n.T("provider_openai_compat")
		}
		cfg.Providers.OpenAICompat = config.OpenAICompatConfig{
			BaseURL:      r.OpenAICompatBaseURL,
			APIKey:       r.OpenAICompatKey,
			Name:         name,
			DefaultModel: r.DefaultModel,
		}
	}

	return cfg
}

// defaultWorkingDir returns the current working directory, falling back to
// the home directory, then empty string.
func defaultWorkingDir() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return ""
}

// defaultTimezone returns the local IANA timezone name, falling back to "UTC".
func defaultTimezone() string {
	name := time.Local.String()
	if name == "" || name == "Local" {
		return "UTC"
	}
	return name
}
