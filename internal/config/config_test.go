package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigPath(t *testing.T) {
	p, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filepath.Base(p) != "config.yaml" {
		t.Errorf("expected filename config.yaml, got %q", filepath.Base(p))
	}
	if filepath.Base(filepath.Dir(p)) != ".feino" {
		t.Errorf("expected parent dir .feino, got %q", filepath.Base(filepath.Dir(p)))
	}
}

func TestLoad_NonExistent(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	// Should be a zero Config.
	if cfg.Providers.Anthropic.APIKey != "" {
		t.Error("expected empty API key for zero config")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte(":\tbadyaml["), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

func TestLoad_UnknownField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(path, []byte("providers:\n  anthropic:\n    totally_fake_key: oops\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
}

func TestSave_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feino", "config.yaml")

	want := &Config{
		Providers: &ProvidersConfig{
			Anthropic: AnthropicConfig{APIKey: "ant-key", DefaultModel: "claude-opus-4-6"},
			OpenAI:    OpenAIConfig{APIKey: "oai-key", BaseURL: "http://localhost", DefaultModel: "gpt-4o"},
			Gemini:    GeminiConfig{APIKey: "gem-key", DefaultModel: "gemini-pro"},
			Ollama:    OllamaConfig{Host: "http://localhost:11434", DefaultModel: "llama3"},
			OpenAICompat: OpenAICompatConfig{
				BaseURL:      "http://localhost:8000/v1",
				APIKey:       "compat-key",
				Name:         "My vLLM",
				DefaultModel: "mistral-7b",
				DisableTools: new(true),
			},
		},
		Agent: &AgentConfig{
			MaxRetries:              3,
			HighComplexityThreshold: 1000,
			LowComplexityThreshold:  200,
			MetricsPath:             "/tmp/metrics.json",
		},
		Security: &SecurityConfig{
			PermissionLevel:    "write",
			AllowedPaths:       []string{"/tmp", "/home"},
			EnableASTBlacklist: new(true),
		},
		Context: &ContextConfig{
			WorkingDir:       "/workspace",
			GlobalConfigPath: "/home/.feino/config.md",
			MaxBudget:        16000,
		},
		UI: &UIConfig{Theme: "dark", LogLevel: "warn"},
	}

	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.Providers.Anthropic.APIKey != want.Providers.Anthropic.APIKey {
		t.Errorf("Anthropic APIKey: got %q, want %q", got.Providers.Anthropic.APIKey, want.Providers.Anthropic.APIKey)
	}
	if got.Providers.OpenAI.BaseURL != want.Providers.OpenAI.BaseURL {
		t.Errorf("OpenAI BaseURL: got %q, want %q", got.Providers.OpenAI.BaseURL, want.Providers.OpenAI.BaseURL)
	}
	if got.Agent.MaxRetries != want.Agent.MaxRetries {
		t.Errorf("Agent.MaxRetries: got %d, want %d", got.Agent.MaxRetries, want.Agent.MaxRetries)
	}
	if got.Agent.MetricsPath != want.Agent.MetricsPath {
		t.Errorf("Agent.MetricsPath: got %q, want %q", got.Agent.MetricsPath, want.Agent.MetricsPath)
	}
	if got.Security.PermissionLevel != want.Security.PermissionLevel {
		t.Errorf("Security.PermissionLevel: got %q, want %q", got.Security.PermissionLevel, want.Security.PermissionLevel)
	}
	if got.Security.EnableASTBlacklist == nil || !*got.Security.EnableASTBlacklist {
		t.Error("Security.EnableASTBlacklist: expected true")
	}
	if len(got.Security.AllowedPaths) != 2 {
		t.Errorf("Security.AllowedPaths: got %v, want 2 entries", got.Security.AllowedPaths)
	}
	if got.Security.AllowedPaths[0] != "/tmp" || got.Security.AllowedPaths[1] != "/home" {
		t.Errorf("Security.AllowedPaths values: got %v, want [\"/tmp\", \"/home\"]", got.Security.AllowedPaths)
	}
	if got.Context.MaxBudget != want.Context.MaxBudget {
		t.Errorf("Context.MaxBudget: got %d, want %d", got.Context.MaxBudget, want.Context.MaxBudget)
	}
	if got.Context.GlobalConfigPath != want.Context.GlobalConfigPath {
		t.Errorf("Context.GlobalConfigPath: got %q, want %q", got.Context.GlobalConfigPath, want.Context.GlobalConfigPath)
	}
	if got.UI.Theme != want.UI.Theme {
		t.Errorf("UI.Theme: got %q, want %q", got.UI.Theme, want.UI.Theme)
	}
	if got.UI.LogLevel != want.UI.LogLevel {
		t.Errorf("UI.LogLevel: got %q, want %q", got.UI.LogLevel, want.UI.LogLevel)
	}
	// OpenAICompat round-trip.
	wc := want.Providers.OpenAICompat
	gc := got.Providers.OpenAICompat
	if gc.BaseURL != wc.BaseURL {
		t.Errorf("OpenAICompat.BaseURL: got %q, want %q", gc.BaseURL, wc.BaseURL)
	}
	if gc.APIKey != wc.APIKey {
		t.Errorf("OpenAICompat.APIKey: got %q, want %q", gc.APIKey, wc.APIKey)
	}
	if gc.Name != wc.Name {
		t.Errorf("OpenAICompat.Name: got %q, want %q", gc.Name, wc.Name)
	}
	if gc.DefaultModel != wc.DefaultModel {
		t.Errorf("OpenAICompat.DefaultModel: got %q, want %q", gc.DefaultModel, wc.DefaultModel)
	}
	if gc.DisableTools == nil || !*gc.DisableTools {
		t.Error("OpenAICompat.DisableTools: expected true after round-trip")
	}
}

func TestSave_FilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, &Config{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode: got %04o, want 0600", mode)
	}
}

func TestSave_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "config.yaml")
	if err := Save(path, &Config{}); err != nil {
		t.Fatalf("Save should create parent dirs: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file should exist after Save: %v", err)
	}
}

func TestMerge_NonZeroOverrideWins(t *testing.T) {
	base := &Config{
		Providers: &ProvidersConfig{
			Anthropic: AnthropicConfig{APIKey: "base-key", DefaultModel: "base-model"},
		},
		Agent: &AgentConfig{MaxRetries: 5},
	}
	override := &Config{
		Providers: &ProvidersConfig{
			Anthropic: AnthropicConfig{APIKey: "override-key"},
		},
		Agent: &AgentConfig{MaxRetries: 10},
	}

	got := Merge(base, override)

	if got.Providers.Anthropic.APIKey != "override-key" {
		t.Errorf("APIKey: got %q, want %q", got.Providers.Anthropic.APIKey, "override-key")
	}
	// DefaultModel is only in base — should be preserved.
	if got.Providers.Anthropic.DefaultModel != "base-model" {
		t.Errorf("DefaultModel: got %q, want %q", got.Providers.Anthropic.DefaultModel, "base-model")
	}
	if got.Agent.MaxRetries != 10 {
		t.Errorf("MaxRetries: got %d, want 10", got.Agent.MaxRetries)
	}
}

func TestMerge_ZeroOverrideKeepsBase(t *testing.T) {
	base := &Config{
		Security: &SecurityConfig{
			PermissionLevel: "bash",
			AllowedPaths:    []string{"/tmp"},
		},
		Context: &ContextConfig{MaxBudget: 8000},
	}
	got := Merge(base, &Config{})

	if got.Security.PermissionLevel != "bash" {
		t.Errorf("PermissionLevel: got %q, want %q", got.Security.PermissionLevel, "bash")
	}
	if len(got.Security.AllowedPaths) != 1 {
		t.Errorf("AllowedPaths: got %v, want 1 entry", got.Security.AllowedPaths)
	}
	if got.Context.MaxBudget != 8000 {
		t.Errorf("MaxBudget: got %d, want 8000", got.Context.MaxBudget)
	}
}

func TestMerge_BoolFlagSetByOverride(t *testing.T) {
	base := &Config{Security: &SecurityConfig{EnableASTBlacklist: new(false)}}
	override := &Config{Security: &SecurityConfig{EnableASTBlacklist: new(true)}}
	got := Merge(base, override)
	if got.Security.EnableASTBlacklist == nil || !*got.Security.EnableASTBlacklist {
		t.Error("EnableASTBlacklist: expected true after override")
	}
}

func TestMerge_BoolFlagCanTurnOff(t *testing.T) {
	base := &Config{Security: &SecurityConfig{EnableASTBlacklist: new(true)}}
	override := &Config{Security: &SecurityConfig{EnableASTBlacklist: new(false)}}
	got := Merge(base, override)
	if got.Security.EnableASTBlacklist == nil || *got.Security.EnableASTBlacklist {
		t.Error("EnableASTBlacklist: expected false after override with BoolPtr(false)")
	}
}

func TestMerge_BoolFlagNilOverrideKeepsBase(t *testing.T) {
	base := &Config{Security: &SecurityConfig{EnableASTBlacklist: new(true)}}
	got := Merge(base, &Config{}) // override has nil EnableASTBlacklist
	if got.Security.EnableASTBlacklist == nil || !*got.Security.EnableASTBlacklist {
		t.Error("EnableASTBlacklist: expected base value (true) when override is nil")
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "ant-from-env")
	t.Setenv("OPENAI_API_KEY", "oai-from-env")
	t.Setenv("GEMINI_API_KEY", "gem-from-env")
	t.Setenv("OLLAMA_HOST", "http://ollama:11434")
	t.Setenv("OPENAI_BASE_URL", "http://proxy")
	t.Setenv("FEINO_LOG_LEVEL", "debug")
	t.Setenv("OPENAI_COMPAT_BASE_URL", "http://localhost:8000/v1")
	t.Setenv("OPENAI_COMPAT_API_KEY", "compat-key")
	t.Setenv("OPENAI_COMPAT_NAME", "My vLLM")

	cfg := FromEnv()

	if cfg.Providers.Anthropic.APIKey != "ant-from-env" {
		t.Errorf("Anthropic.APIKey: got %q", cfg.Providers.Anthropic.APIKey)
	}
	if cfg.Providers.OpenAI.APIKey != "oai-from-env" {
		t.Errorf("OpenAI.APIKey: got %q", cfg.Providers.OpenAI.APIKey)
	}
	if cfg.Providers.Gemini.APIKey != "gem-from-env" {
		t.Errorf("Gemini.APIKey: got %q", cfg.Providers.Gemini.APIKey)
	}
	if cfg.Providers.Ollama.Host != "http://ollama:11434" {
		t.Errorf("Ollama.Host: got %q", cfg.Providers.Ollama.Host)
	}
	if cfg.Providers.OpenAI.BaseURL != "http://proxy" {
		t.Errorf("OpenAI.BaseURL: got %q", cfg.Providers.OpenAI.BaseURL)
	}
	if cfg.UI.LogLevel != "debug" {
		t.Errorf("UI.LogLevel: got %q, want %q", cfg.UI.LogLevel, "debug")
	}
	if cfg.Providers.OpenAICompat.BaseURL != "http://localhost:8000/v1" {
		t.Errorf("OpenAICompat.BaseURL: got %q", cfg.Providers.OpenAICompat.BaseURL)
	}
	if cfg.Providers.OpenAICompat.APIKey != "compat-key" {
		t.Errorf("OpenAICompat.APIKey: got %q", cfg.Providers.OpenAICompat.APIKey)
	}
	if cfg.Providers.OpenAICompat.Name != "My vLLM" {
		t.Errorf("OpenAICompat.Name: got %q", cfg.Providers.OpenAICompat.Name)
	}
}

func TestFromEnv_EmptyWhenUnset(t *testing.T) {
	// Clear all relevant env vars.
	for _, k := range []string{
		"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY",
		"OLLAMA_HOST", "OPENAI_BASE_URL", "FEINO_LOG_LEVEL",
		"OPENAI_COMPAT_BASE_URL", "OPENAI_COMPAT_API_KEY", "OPENAI_COMPAT_NAME",
	} {
		t.Setenv(k, "")
	}
	cfg := FromEnv()
	if cfg.Providers.Anthropic.APIKey != "" ||
		cfg.Providers.OpenAI.APIKey != "" ||
		cfg.Providers.Gemini.APIKey != "" ||
		cfg.Providers.OpenAICompat.BaseURL != "" ||
		cfg.Providers.OpenAICompat.APIKey != "" {
		t.Error("expected all keys/URLs empty when env vars are unset")
	}
}

func TestParseLogLevel(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"DEBUG", slog.LevelDebug},
		{"INFO", slog.LevelInfo},
		{"WARN", slog.LevelWarn},
		{"ERROR", slog.LevelError},
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
	}
	for _, tc := range cases {
		if got := ParseLogLevel(tc.in); got != tc.want {
			t.Errorf("ParseLogLevel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestMerge_GeminiVertex(t *testing.T) {
	// Turning Vertex on via override.
	base := &Config{}
	override := &Config{Providers: &ProvidersConfig{Gemini: GeminiConfig{
		Vertex:    new(true),
		ProjectID: "my-proj",
		Location:  "us-central1",
	}}}
	got := Merge(base, override)
	if got.Providers.Gemini.Vertex == nil || !*got.Providers.Gemini.Vertex {
		t.Error("Vertex: expected true after override")
	}
	if got.Providers.Gemini.ProjectID != "my-proj" {
		t.Errorf("ProjectID: got %q, want %q", got.Providers.Gemini.ProjectID, "my-proj")
	}

	// Explicitly turning Vertex off via override (switching back to API key).
	base2 := &Config{Providers: &ProvidersConfig{Gemini: GeminiConfig{Vertex: new(true), ProjectID: "old-proj"}}}
	override2 := &Config{Providers: &ProvidersConfig{Gemini: GeminiConfig{Vertex: new(false), APIKey: "new-key"}}}
	got2 := Merge(base2, override2)
	if got2.Providers.Gemini.Vertex == nil || *got2.Providers.Gemini.Vertex {
		t.Error("Vertex: expected false after explicit false override")
	}
	if got2.Providers.Gemini.APIKey != "new-key" {
		t.Errorf("APIKey: got %q, want %q", got2.Providers.Gemini.APIKey, "new-key")
	}

	// Nil override keeps base.
	base3 := &Config{Providers: &ProvidersConfig{Gemini: GeminiConfig{Vertex: new(true)}}}
	got3 := Merge(base3, &Config{})
	if got3.Providers.Gemini.Vertex == nil || !*got3.Providers.Gemini.Vertex {
		t.Error("Vertex: expected base value (true) preserved when override is nil")
	}
}

func TestMerge_UITheme(t *testing.T) {
	base := &Config{UI: &UIConfig{Theme: "dark"}}
	override := &Config{UI: &UIConfig{Theme: "light"}}
	got := Merge(base, override)
	if got.UI.Theme != "light" {
		t.Errorf("UI.Theme: got %q, want %q", got.UI.Theme, "light")
	}

	// Zero override keeps base.
	got2 := Merge(base, &Config{})
	if got2.UI.Theme != "dark" {
		t.Errorf("UI.Theme: got %q, want %q (zero override should keep base)", got2.UI.Theme, "dark")
	}
}

func TestMerge_OpenAICompatDisableTools(t *testing.T) {
	// Setting DisableTools via override.
	base := &Config{}
	override := &Config{Providers: &ProvidersConfig{OpenAICompat: OpenAICompatConfig{
		BaseURL:      "http://localhost:8000/v1",
		DisableTools: new(true),
	}}}
	got := Merge(base, override)
	if got.Providers.OpenAICompat.DisableTools == nil || !*got.Providers.OpenAICompat.DisableTools {
		t.Error("DisableTools: expected true after override")
	}

	// Explicitly disabling with false overrides a true base.
	base2 := &Config{Providers: &ProvidersConfig{OpenAICompat: OpenAICompatConfig{DisableTools: new(true)}}}
	override2 := &Config{Providers: &ProvidersConfig{OpenAICompat: OpenAICompatConfig{DisableTools: new(false)}}}
	got2 := Merge(base2, override2)
	if got2.Providers.OpenAICompat.DisableTools == nil || *got2.Providers.OpenAICompat.DisableTools {
		t.Error("DisableTools: expected false after explicit false override")
	}

	// Nil override keeps base.
	base3 := &Config{Providers: &ProvidersConfig{OpenAICompat: OpenAICompatConfig{DisableTools: new(true)}}}
	got3 := Merge(base3, &Config{})
	if got3.Providers.OpenAICompat.DisableTools == nil || !*got3.Providers.OpenAICompat.DisableTools {
		t.Error("DisableTools: expected base value (true) preserved when override is nil")
	}
}

func TestMerge_EmailEnabled(t *testing.T) {
	// Setting enabled via override.
	base := &Config{}
	override := &Config{Services: &ServicesConfig{Email: EmailServiceConfig{Enabled: new(true)}}}
	got := Merge(base, override)
	if got.Services.Email.Enabled == nil || !*got.Services.Email.Enabled {
		t.Error("Email.Enabled: expected true after override")
	}

	// Explicitly disabling overrides true base.
	base2 := &Config{Services: &ServicesConfig{Email: EmailServiceConfig{Enabled: new(true)}}}
	override2 := &Config{Services: &ServicesConfig{Email: EmailServiceConfig{Enabled: new(false)}}}
	got2 := Merge(base2, override2)
	if got2.Services.Email.Enabled == nil || *got2.Services.Email.Enabled {
		t.Error("Email.Enabled: expected false after explicit false override")
	}

	// Nil override keeps base.
	base3 := &Config{Services: &ServicesConfig{Email: EmailServiceConfig{Enabled: new(true)}}}
	got3 := Merge(base3, &Config{})
	if got3.Services.Email.Enabled == nil || !*got3.Services.Email.Enabled {
		t.Error("Email.Enabled: expected base value (true) preserved when override is nil")
	}
}

func TestHasCredentials(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want bool
	}{
		{
			name: "empty config has no credentials",
			cfg:  &Config{},
			want: false,
		},
		{
			name: "anthropic API key",
			cfg:  &Config{Providers: &ProvidersConfig{Anthropic: AnthropicConfig{APIKey: "sk-ant"}}},
			want: true,
		},
		{
			name: "openai API key",
			cfg:  &Config{Providers: &ProvidersConfig{OpenAI: OpenAIConfig{APIKey: "sk-oai"}}},
			want: true,
		},
		{
			name: "gemini API key",
			cfg:  &Config{Providers: &ProvidersConfig{Gemini: GeminiConfig{APIKey: "gem-key"}}},
			want: true,
		},
		{
			name: "gemini vertex complete",
			cfg: &Config{Providers: &ProvidersConfig{Gemini: GeminiConfig{
				Vertex:    new(true),
				ProjectID: "my-project",
				Location:  "us-central1",
			}}},
			want: true,
		},
		{
			name: "gemini vertex missing project — not enough",
			cfg: &Config{Providers: &ProvidersConfig{Gemini: GeminiConfig{
				Vertex:   new(true),
				Location: "us-central1",
			}}},
			want: false,
		},
		{
			name: "gemini vertex missing location — not enough",
			cfg: &Config{Providers: &ProvidersConfig{Gemini: GeminiConfig{
				Vertex:    new(true),
				ProjectID: "my-project",
			}}},
			want: false,
		},
		{
			name: "ollama default model set",
			cfg:  &Config{Providers: &ProvidersConfig{Ollama: OllamaConfig{DefaultModel: "llama3"}}},
			want: true,
		},
		{
			name: "openai_compat base url set",
			cfg:  &Config{Providers: &ProvidersConfig{OpenAICompat: OpenAICompatConfig{BaseURL: "http://localhost:8000/v1"}}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasCredentials(tt.cfg); got != tt.want {
				t.Errorf("HasCredentials() = %v, want %v", got, tt.want)
			}
		})
	}
}
