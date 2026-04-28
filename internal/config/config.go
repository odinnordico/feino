package config

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// BoolPtr is a convenience helper that returns a pointer to b.
// Use it when setting pointer-bool fields in a Config literal:
//
//	SecurityConfig{EnableASTBlacklist: config.BoolPtr(true)}
//
//go:fix inline
//go:fix inline
//go:fix inline
func BoolPtr(b bool) *bool { return new(b) }

// Config is the top-level application configuration. All fields use zero-value
// defaults so callers may construct Config{} and override only what they need.
type Config struct {
	Providers *ProvidersConfig   `yaml:"providers"`
	Agent     *AgentConfig       `yaml:"agent"`
	Security  *SecurityConfig    `yaml:"security"`
	Context   *ContextConfig     `yaml:"context"`
	MCP       *MCPConfig         `yaml:"mcp"`
	UI        *UIConfig          `yaml:"ui"`
	User      *UserProfileConfig `yaml:"user,omitempty"`
	Services  *ServicesConfig    `yaml:"services,omitempty"`
}

// Defaults initialises every nil pointer field in c to its zero-value struct
// so that downstream code can safely dereference without nil checks. It is
// idempotent and safe to call multiple times.
func (c *Config) Defaults() {
	if c.Providers == nil {
		c.Providers = &ProvidersConfig{}
	}
	if c.Agent == nil {
		c.Agent = &AgentConfig{}
	}
	if c.Security == nil {
		c.Security = &SecurityConfig{}
	}
	if c.Context == nil {
		c.Context = &ContextConfig{}
	}
	if c.MCP == nil {
		c.MCP = &MCPConfig{}
	}
	if c.UI == nil {
		c.UI = &UIConfig{}
	}
	if c.User == nil {
		c.User = &UserProfileConfig{}
	}
	if c.Services == nil {
		c.Services = &ServicesConfig{}
	}
}

// UserProfileConfig holds identity and behavioural preferences for the user.
// These values are injected into every system prompt so the agent can
// personalise responses without the user repeating themselves.
type UserProfileConfig struct {
	// Name is how the user prefers to be addressed ("Diego", "Dr Smith", etc.).
	Name string `yaml:"name,omitempty"`
	// Timezone is an IANA timezone name, e.g. "America/Bogota" or "Europe/Berlin".
	// When set, the agent uses it to interpret relative times and schedule tasks.
	Timezone string `yaml:"timezone,omitempty"`
	// CommunicationStyle controls how the agent formats responses.
	// Valid values: "concise", "detailed", "technical", "friendly".
	// Empty string leaves the agent's default style in effect.
	CommunicationStyle string `yaml:"communication_style,omitempty"`
}

// communicationStyleHint maps a CommunicationStyle value to the instruction
// injected into the prompt. Kept here so config and context stay in sync.
var communicationStyleHints = map[string]string{
	"concise":   "Be brief. Use bullet points over prose. Skip pleasantries and preambles.",
	"detailed":  "Provide thorough explanations with context, rationale, and examples.",
	"technical": "Assume deep domain expertise. Skip introductory material, use precise terminology, prefer code over prose.",
	"friendly":  "Use a warm, conversational tone. Explain concepts from first principles. Be approachable.",
}

// FormatPrompt renders the profile as a multi-line string for system-prompt
// injection. Returns an empty string when no profile fields are set.
func (u UserProfileConfig) FormatPrompt() string {
	var lines []string
	if u.Name != "" {
		lines = append(lines, "Name: "+u.Name)
	}
	if u.Timezone != "" {
		lines = append(lines, "Timezone: "+u.Timezone)
	}
	if u.CommunicationStyle != "" {
		hint := communicationStyleHints[u.CommunicationStyle]
		if hint == "" {
			hint = u.CommunicationStyle
		}
		lines = append(lines, "Communication style: "+u.CommunicationStyle+" — "+hint)
	}
	return strings.Join(lines, "\n")
}

// UIConfig holds user-interface preferences.
type UIConfig struct {
	Theme    string `yaml:"theme"`     // "dark" | "light" | "auto"
	LogLevel string `yaml:"log_level"` // "debug" | "info" | "warn" | "error"; default "info"
	Language string `yaml:"language"`  // BCP 47 tag, e.g. "en", "es-419", "pt-BR"; "" = auto-detect from $LANG
}

// ProvidersConfig holds credentials and default model preferences for each
// supported LLM provider.
type ProvidersConfig struct {
	Anthropic    AnthropicConfig    `yaml:"anthropic"`
	OpenAI       OpenAIConfig       `yaml:"openai"`
	Gemini       GeminiConfig       `yaml:"gemini"`
	Ollama       OllamaConfig       `yaml:"ollama"`
	OpenAICompat OpenAICompatConfig `yaml:"openai_compat"`
}

// AnthropicConfig holds Anthropic-specific settings.
// APIKey falls back to the ANTHROPIC_API_KEY environment variable when empty.
type AnthropicConfig struct {
	APIKey       string `yaml:"api_key"`
	DefaultModel string `yaml:"default_model"`
}

// OpenAIConfig holds OpenAI-specific settings.
// APIKey falls back to the OPENAI_API_KEY environment variable when empty.
type OpenAIConfig struct {
	APIKey       string `yaml:"api_key"`
	BaseURL      string `yaml:"base_url"` // override for proxies/local servers
	DefaultModel string `yaml:"default_model"`
}

// GeminiConfig holds Google Gemini-specific settings.
// APIKey falls back to the GEMINI_API_KEY environment variable when empty.
// When Vertex is true, APIKey is ignored; ProjectID and Location are used
// for Vertex AI authentication via Application Default Credentials.
// Vertex uses *bool so Merge can distinguish "not set" (nil) from
// "explicitly disabled" (false), the same pattern as EnableASTBlacklist.
type GeminiConfig struct {
	APIKey       string `yaml:"api_key"`
	DefaultModel string `yaml:"default_model"`
	Vertex       *bool  `yaml:"vertex,omitempty"`
	ProjectID    string `yaml:"project_id"`
	Location     string `yaml:"location"`
}

// OllamaConfig holds Ollama-specific settings.
// Host defaults to http://localhost:11434 when empty.
type OllamaConfig struct {
	Host         string `yaml:"host"`
	DefaultModel string `yaml:"default_model"`
}

// OpenAICompatConfig holds settings for a generic OpenAI-compatible API
// endpoint. Supported servers include vLLM, LocalAI, LM Studio, Llamafile,
// and any service implementing the OpenAI Chat Completions API.
//
// BaseURL is required — it points at the root of the API, e.g.
// "http://localhost:8000/v1". APIKey is optional; many local servers do not
// require authentication. Name is the display label shown in the TUI.
//
// The following environment variables override the file-based config:
//
//	OPENAI_COMPAT_BASE_URL — base URL of the endpoint
//	OPENAI_COMPAT_API_KEY  — Bearer token (optional)
//	OPENAI_COMPAT_NAME     — display name (optional)
type OpenAICompatConfig struct {
	BaseURL      string `yaml:"base_url"`      // required, e.g. http://localhost:8000/v1
	APIKey       string `yaml:"api_key"`       // optional Bearer token
	Name         string `yaml:"name"`          // optional display label
	DefaultModel string `yaml:"default_model"` // optional pre-selected model
	// DisableTools prevents advertising tool/function-calling support.
	// Set to true for servers or models that do not implement the `tools`
	// field in chat completions (e.g. older llama.cpp builds).
	// Uses *bool so Merge can distinguish "not set" (nil) from "explicitly
	// disabled" (false), consistent with EnableASTBlacklist.
	DisableTools *bool `yaml:"disable_tools,omitempty"`
}

// AgentConfig tunes the ReAct agent and TACOS router behaviour.
// Zero values fall back to the subsystem defaults (MaxRetries=5,
// HighComplexityThreshold=2000, LowComplexityThreshold=500).
type AgentConfig struct {
	MaxRetries              int    `yaml:"max_retries"`
	HighComplexityThreshold int    `yaml:"high_complexity_threshold"`
	LowComplexityThreshold  int    `yaml:"low_complexity_threshold"`
	MetricsPath             string `yaml:"metrics_path"` // default ~/.feino/metrics.json
}

// SecurityConfig controls the permission gate, path allowlisting, and AST
// shell command scanning.
type SecurityConfig struct {
	PermissionLevel string   `yaml:"permission_level"` // read|write|bash|danger_zone
	AllowedPaths    []string `yaml:"allowed_paths"`
	// EnableASTBlacklist uses a pointer so Merge can distinguish between
	// "not set by this layer" (nil) and "explicitly disabled" (false).
	// Use config.BoolPtr(true/false) when constructing a literal.
	EnableASTBlacklist *bool `yaml:"enable_ast_blacklist"`
}

// ContextConfig controls context assembly, the working directory, and the
// character budget for assembled prompts.
type ContextConfig struct {
	WorkingDir       string `yaml:"working_dir"`        // default: cwd
	GlobalConfigPath string `yaml:"global_config_path"` // default: ~/.feino/config.md
	MaxBudget        int    `yaml:"max_budget"`         // default: 32000
	// PluginsDir is the directory feino scans for script plugins at startup.
	// Each plugin is a JSON manifest + matching executable (see docs/plugins.md).
	// Defaults to ~/.feino/plugins when empty.
	PluginsDir string `yaml:"plugins_dir"`
}

// MCPConfig lists Model Context Protocol servers to connect at startup.
type MCPConfig struct {
	Servers []MCPServerConfig `yaml:"servers"`
}

// ServicesConfig holds configuration for optional service integrations.
// Only non-sensitive connection settings live here; credentials (passwords,
// OAuth tokens) are kept in the OS keyring or encrypted-file credential store
// (see internal/credentials). Never write secrets to this struct.
type ServicesConfig struct {
	Email EmailServiceConfig `yaml:"email,omitempty"`
}

// EmailServiceConfig holds IMAP/SMTP connection settings for the email tools.
// The username and password are stored in the credential store under service
// "email", keys "username" and "password". Configure via the /email-setup
// wizard rather than editing this file directly.
type EmailServiceConfig struct {
	// Enabled uses *bool so that Merge can distinguish "not set" (nil) from
	// "explicitly disabled" (false), consistent with EnableASTBlacklist.
	// Use config.BoolPtr(true/false) when constructing a literal.
	Enabled *bool `yaml:"enabled,omitempty"`
	// Address is the From address used when sending and the display address
	// shown in the TUI. Usually identical to the IMAP/SMTP username.
	Address string `yaml:"address"`
	// IMAPHost is the hostname of the IMAP server, e.g. "imap.gmail.com".
	IMAPHost string `yaml:"imap_host"`
	// IMAPPort is the IMAP server port. 993 (IMAPS/TLS) is the default;
	// 143 triggers STARTTLS.
	IMAPPort int `yaml:"imap_port"`
	// SMTPHost is the hostname of the SMTP server, e.g. "smtp.gmail.com".
	SMTPHost string `yaml:"smtp_host"`
	// SMTPPort is the SMTP server port. 587 (STARTTLS) is the default;
	// 465 uses implicit TLS (SMTPS).
	SMTPPort int `yaml:"smtp_port"`
}

// MCPServerConfig describes one MCP server connection.
type MCPServerConfig struct {
	Name      string            `yaml:"name"`
	Transport string            `yaml:"transport"` // stdio | sse
	Command   string            `yaml:"command"`   // stdio: executable path
	Args      []string          `yaml:"args"`
	URL       string            `yaml:"url"` // sse: endpoint URL
	Env       map[string]string `yaml:"env"`
}

// ParseLogLevel converts a string log level name to its slog.Level equivalent.
// Matching is case-insensitive. Unrecognised values (including "") return slog.LevelInfo.
func ParseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// FeinoDir returns the path to the ~/.feino directory, creating it if needed.
func FeinoDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: cannot determine home directory: %w", err)
	}
	dir := filepath.Join(home, ".feino")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("config: create feino directory: %w", err)
	}
	return dir, nil
}

// DefaultConfigPath returns the canonical YAML config file path: ~/.feino/config.yaml.
func DefaultConfigPath() (string, error) {
	dir, err := FeinoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// Load reads and parses a YAML config file from path. If the file does not
// exist, a zero Config is returned without error — this is the expected
// first-run case. Unknown YAML keys are rejected to surface typos early.
func Load(path string) (*Config, error) {
	cfg := &Config{}
	cfg.Defaults()

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("config: read %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return cfg, nil // empty file is valid; treat as zero config
		}
		return cfg, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return cfg, nil
}

// Save marshals cfg as YAML and writes it atomically to path, creating parent
// directories as needed. The file is written with mode 0600 to protect API
// keys. Atomicity is achieved by writing to a temporary file in the same
// directory and then renaming it, so a crash mid-write never corrupts the
// existing config.
func Save(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("config: create dir for %s: %w", path, err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	// Write to a temp file in the same directory so the rename is on the same
	// filesystem and therefore atomic.
	tmp, err := os.CreateTemp(dir, ".feino-config-*.tmp")
	if err != nil {
		return fmt.Errorf("config: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	// Clean up the temp file on any failure path.
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("config: chmod temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("config: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("config: rename to %s: %w", path, err)
	}
	tmpName = "" // rename succeeded; suppress the deferred Remove
	return nil
}

// ── Merge helpers ─────────────────────────────────────────────────────────────
// Package-level helpers shared by Merge and mergeEmail. Each returns the
// override value when it is non-zero, otherwise the base value.

func mergeStr(b, o string) string {
	if o != "" {
		return o
	}
	return b
}

func mergeNum(b, o int) int {
	if o != 0 {
		return o
	}
	return b
}

func mergeBoolPtr(b, o *bool) *bool {
	if o != nil {
		return o
	}
	return b
}

func mergeStrs(b, o []string) []string {
	if len(o) > 0 {
		return o
	}
	return b
}

func mergeServers(b, o []MCPServerConfig) []MCPServerConfig {
	if len(o) > 0 {
		return o
	}
	return b
}

// Merge returns a new Config where every non-zero field in override replaces
// the corresponding field in base. Use Merge(fileConfig, FromEnv()) to give
// environment variables precedence over stored settings.
//
// For string fields the zero value is "" (not set).
// For int fields the zero value is 0 (not set).
// For []string fields a nil/empty slice means not set.
// For *bool fields nil means not set; non-nil (true or false) always wins.
func Merge(base, override *Config) *Config {
	base.Defaults()
	override.Defaults()

	return &Config{
		Providers: &ProvidersConfig{
			Anthropic: AnthropicConfig{
				APIKey:       mergeStr(base.Providers.Anthropic.APIKey, override.Providers.Anthropic.APIKey),
				DefaultModel: mergeStr(base.Providers.Anthropic.DefaultModel, override.Providers.Anthropic.DefaultModel),
			},
			OpenAI: OpenAIConfig{
				APIKey:       mergeStr(base.Providers.OpenAI.APIKey, override.Providers.OpenAI.APIKey),
				BaseURL:      mergeStr(base.Providers.OpenAI.BaseURL, override.Providers.OpenAI.BaseURL),
				DefaultModel: mergeStr(base.Providers.OpenAI.DefaultModel, override.Providers.OpenAI.DefaultModel),
			},
			Gemini: GeminiConfig{
				APIKey:       mergeStr(base.Providers.Gemini.APIKey, override.Providers.Gemini.APIKey),
				DefaultModel: mergeStr(base.Providers.Gemini.DefaultModel, override.Providers.Gemini.DefaultModel),
				Vertex:       mergeBoolPtr(base.Providers.Gemini.Vertex, override.Providers.Gemini.Vertex),
				ProjectID:    mergeStr(base.Providers.Gemini.ProjectID, override.Providers.Gemini.ProjectID),
				Location:     mergeStr(base.Providers.Gemini.Location, override.Providers.Gemini.Location),
			},
			Ollama: OllamaConfig{
				Host:         mergeStr(base.Providers.Ollama.Host, override.Providers.Ollama.Host),
				DefaultModel: mergeStr(base.Providers.Ollama.DefaultModel, override.Providers.Ollama.DefaultModel),
			},
			OpenAICompat: OpenAICompatConfig{
				BaseURL:      mergeStr(base.Providers.OpenAICompat.BaseURL, override.Providers.OpenAICompat.BaseURL),
				APIKey:       mergeStr(base.Providers.OpenAICompat.APIKey, override.Providers.OpenAICompat.APIKey),
				Name:         mergeStr(base.Providers.OpenAICompat.Name, override.Providers.OpenAICompat.Name),
				DefaultModel: mergeStr(base.Providers.OpenAICompat.DefaultModel, override.Providers.OpenAICompat.DefaultModel),
				DisableTools: mergeBoolPtr(base.Providers.OpenAICompat.DisableTools, override.Providers.OpenAICompat.DisableTools),
			},
		},
		Agent: &AgentConfig{
			MaxRetries:              mergeNum(base.Agent.MaxRetries, override.Agent.MaxRetries),
			HighComplexityThreshold: mergeNum(base.Agent.HighComplexityThreshold, override.Agent.HighComplexityThreshold),
			LowComplexityThreshold:  mergeNum(base.Agent.LowComplexityThreshold, override.Agent.LowComplexityThreshold),
			MetricsPath:             mergeStr(base.Agent.MetricsPath, override.Agent.MetricsPath),
		},
		Security: &SecurityConfig{
			PermissionLevel:    mergeStr(base.Security.PermissionLevel, override.Security.PermissionLevel),
			AllowedPaths:       mergeStrs(base.Security.AllowedPaths, override.Security.AllowedPaths),
			EnableASTBlacklist: mergeBoolPtr(base.Security.EnableASTBlacklist, override.Security.EnableASTBlacklist),
		},
		Context: &ContextConfig{
			WorkingDir:       mergeStr(base.Context.WorkingDir, override.Context.WorkingDir),
			GlobalConfigPath: mergeStr(base.Context.GlobalConfigPath, override.Context.GlobalConfigPath),
			MaxBudget:        mergeNum(base.Context.MaxBudget, override.Context.MaxBudget),
			PluginsDir:       mergeStr(base.Context.PluginsDir, override.Context.PluginsDir),
		},
		MCP: &MCPConfig{
			Servers: mergeServers(base.MCP.Servers, override.MCP.Servers),
		},
		UI: &UIConfig{
			Theme:    mergeStr(base.UI.Theme, override.UI.Theme),
			LogLevel: mergeStr(base.UI.LogLevel, override.UI.LogLevel),
			Language: mergeStr(base.UI.Language, override.UI.Language),
		},
		User: &UserProfileConfig{
			Name:               mergeStr(base.User.Name, override.User.Name),
			Timezone:           mergeStr(base.User.Timezone, override.User.Timezone),
			CommunicationStyle: mergeStr(base.User.CommunicationStyle, override.User.CommunicationStyle),
		},
		Services: &ServicesConfig{
			Email: mergeEmail(base.Services.Email, override.Services.Email),
		},
	}
}

// mergeEmail merges two EmailServiceConfig values using the same non-zero-wins
// semantics as Merge. Enabled uses *bool so an explicit false can override true.
func mergeEmail(base, override EmailServiceConfig) EmailServiceConfig {
	return EmailServiceConfig{
		Enabled:  mergeBoolPtr(base.Enabled, override.Enabled),
		Address:  mergeStr(base.Address, override.Address),
		IMAPHost: mergeStr(base.IMAPHost, override.IMAPHost),
		IMAPPort: mergeNum(base.IMAPPort, override.IMAPPort),
		SMTPHost: mergeStr(base.SMTPHost, override.SMTPHost),
		SMTPPort: mergeNum(base.SMTPPort, override.SMTPPort),
	}
}

// FromEnv returns a Config populated from well-known environment variables.
// Only non-empty values are set so that Merge(base, FromEnv()) gives env
// variables precedence without erasing file-only settings.
//
// Variables read: ANTHROPIC_API_KEY, OPENAI_API_KEY, OPENAI_BASE_URL,
// GEMINI_API_KEY, OLLAMA_HOST, OPENAI_COMPAT_BASE_URL, OPENAI_COMPAT_API_KEY,
// OPENAI_COMPAT_NAME, FEINO_LOG_LEVEL.
//
// Note: OPENAI_COMPAT_DISABLE_TOOLS has no env-var equivalent because the
// flag is structural (it disables a feature of the provider configuration)
// rather than a credential. Set it via the config file or wizard instead.
func FromEnv() *Config {
	return &Config{
		Providers: &ProvidersConfig{
			Anthropic: AnthropicConfig{
				APIKey: os.Getenv("ANTHROPIC_API_KEY"),
			},
			OpenAI: OpenAIConfig{
				APIKey:  os.Getenv("OPENAI_API_KEY"),
				BaseURL: os.Getenv("OPENAI_BASE_URL"),
			},
			Gemini: GeminiConfig{
				APIKey: os.Getenv("GEMINI_API_KEY"),
			},
			Ollama: OllamaConfig{
				Host: os.Getenv("OLLAMA_HOST"),
			},
			OpenAICompat: OpenAICompatConfig{
				BaseURL: os.Getenv("OPENAI_COMPAT_BASE_URL"),
				APIKey:  os.Getenv("OPENAI_COMPAT_API_KEY"),
				Name:    os.Getenv("OPENAI_COMPAT_NAME"),
			},
		},
		UI: &UIConfig{
			LogLevel: os.Getenv("FEINO_LOG_LEVEL"),
		},
	}
}

// HasCredentials reports whether cfg contains at least one usable provider
// credential. A Gemini Vertex config without an API key is considered a valid
// credential when both ProjectID and Location are set.
func HasCredentials(cfg *Config) bool {
	cfg.Defaults()
	p := cfg.Providers
	if p.Anthropic.APIKey != "" {
		return true
	}
	if p.OpenAI.APIKey != "" {
		return true
	}
	if p.Gemini.APIKey != "" {
		return true
	}
	if p.Gemini.Vertex != nil && *p.Gemini.Vertex && p.Gemini.ProjectID != "" && p.Gemini.Location != "" {
		return true
	}
	if p.Ollama.DefaultModel != "" {
		return true
	}
	if p.OpenAICompat.BaseURL != "" {
		return true
	}
	return false
}
