# Package `internal/config`

The `config` package owns the application's configuration schema, persistence, and environment variable overlay. It is the single source of truth for all settings; every other package receives a `Config` value and never touches disk directly.

---

## Schema overview

```
Config
├── Providers       ProvidersConfig
│   ├── Anthropic   AnthropicConfig      (APIKey, DefaultModel)
│   ├── OpenAI      OpenAIConfig         (APIKey, BaseURL, DefaultModel)
│   ├── Gemini      GeminiConfig         (APIKey, Vertex *bool, ProjectID, Location, DefaultModel)
│   ├── Ollama      OllamaConfig         (Host, DefaultModel)
│   └── OpenAICompat OpenAICompatConfig  (BaseURL, APIKey, Name, DefaultModel)
├── Agent           AgentConfig          (MaxRetries, complexity thresholds, MetricsPath)
├── Security        SecurityConfig       (PermissionLevel, AllowedPaths, EnableASTBlacklist *bool)
├── Context         ContextConfig        (WorkingDir, GlobalConfigPath, MaxBudget, PluginsDir)
├── MCP             MCPConfig            (Servers []MCPServerConfig)
├── UI              UIConfig             (Theme, LogLevel, Language)
├── User            UserProfileConfig    (Name, Timezone, CommunicationStyle)
└── Services        ServicesConfig
    └── Email       EmailServiceConfig
```

All fields have sensible zero values; `Config{}` is a valid configuration.

---

## Key API

### Loading and saving

```go
// Load parses ~/.feino/config.yaml (or any path).
// Returns zero-value Config without error when the file is missing.
cfg, err := config.Load(path)

// Save writes atomically (temp-file + rename).
err = config.Save(path, cfg)

// Convenience helpers.
path, err := config.DefaultConfigPath() // ~/.feino/config.yaml
dir,  err := config.FeinoDir()          // ~/.feino (created if missing)
```

### Environment overlay

```go
// FromEnv reads well-known environment variables into a Config.
// Supported: ANTHROPIC_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY,
//            OLLAMA_HOST, OPENAI_COMPAT_BASE_URL, FEINO_LOG_LEVEL
envCfg := config.FromEnv()

// Merge applies override on top of base; non-zero values in override win.
// Environment variables always win over file values at startup.
merged := config.Merge(fileCfg, envCfg)
```

### Helpers

```go
// HasCredentials returns true when at least one provider has credentials.
ok := config.HasCredentials(cfg)

// ParseLogLevel converts "debug"/"info"/"warn"/"error" to slog.Level.
level := config.ParseLogLevel("debug")

// BoolPtr wraps a literal bool into a *bool.
// Needed for pointer-bool fields that distinguish nil (unset) from false.
p := config.BoolPtr(true)
```

---

## Pointer-bool fields

`SecurityConfig.EnableASTBlacklist` and `GeminiConfig.Vertex` are `*bool` rather than `bool`. This lets `Merge` distinguish three states:

| Value | Meaning |
|-------|---------|
| `nil` | Not configured; apply default behaviour |
| `*false` | Explicitly disabled |
| `*true` | Explicitly enabled |

Use `config.BoolPtr(true)` in code and `enable_ast_blacklist: true` in YAML.

---

## YAML structure

```yaml
providers:
  anthropic:
    api_key: sk-ant-...          # or set ANTHROPIC_API_KEY
    default_model: claude-opus-4-7
  openai:
    api_key: sk-...              # or set OPENAI_API_KEY
    default_model: gpt-4o
  gemini:
    api_key: AIza...             # or set GEMINI_API_KEY
    default_model: gemini-2.0-flash
  ollama:
    host: http://localhost:11434
    default_model: llama3
  openai_compat:
    base_url: http://localhost:8000/v1
    name: vLLM
    default_model: mistral-7b

agent:
  max_retries: 5

security:
  permission_level: bash         # read | write | bash | danger_zone
  allowed_paths:
    - /home/user/project
  enable_ast_blacklist: true

context:
  working_dir: /home/user/project
  global_config_path: ~/.feino/config.md
  max_budget: 80000
  plugins_dir: ~/.feino/plugins

ui:
  theme: dark                    # dark | light | auto
  log_level: info
  language: en                   # BCP 47 tag

user:
  name: Diego
  timezone: America/Bogota
  communication_style: technical # concise | detailed | technical | friendly
```

---

## Best practices

- **Never embed credentials in binaries or repositories.** Always use environment variables (`ANTHROPIC_API_KEY`, etc.) or the OS keyring (see `internal/credentials`).
- **Use `Merge(Load(...), FromEnv())`** as the standard startup sequence. Environment variables override file settings; file settings override zero values.
- **Validate early.** Call `HasCredentials` after loading and abort with a clear message if no provider is configured.
- **Keep `FeinoDir` as the single path root.** Do not construct `~/.feino/…` paths manually; use `config.FeinoDir()` to ensure consistency across platforms.
- **YAML rejects unknown fields** (strict unmarshal). This is intentional — typos in config keys produce errors rather than silently doing nothing.
- **Do not persist sensitive fields after a `Load`.** The `Save` function writes whatever is in the struct. Strip `APIKey` fields before saving if they should remain env-only.

---

## UserProfileConfig

The user profile is injected into every system prompt via `FormatPrompt()`, enabling the agent to personalise responses without the user repeating themselves:

```go
profile := cfg.User.FormatPrompt()
// → "Name: Diego\nTimezone: America/Bogota\nCommunication style: technical — ..."
```

`CommunicationStyle` values map to specific prompt instructions:

| Value | Injected instruction |
|-------|---------------------|
| `concise` | Brief, bullet points, no pleasantries |
| `detailed` | Thorough explanations with context and examples |
| `technical` | Deep expertise assumed; code over prose |
| `friendly` | Warm, conversational, first-principles explanations |

---

## Extending the schema

1. Add the new field to the appropriate struct in `config.go` with a YAML tag and a doc comment.
2. If the field has an environment variable, add it to `FromEnv`.
3. Update `Merge` to handle the new field (pointer types need an explicit nil check; value types use non-zero logic).
4. Add a test case in `config_test.go` verifying YAML round-trip and env override behaviour.

### Adding a new provider config

```go
type DeepSeekConfig struct {
    APIKey       string `yaml:"api_key"`
    DefaultModel string `yaml:"default_model"`
}
```

Add `DeepSeek DeepSeekConfig` to `ProvidersConfig`, update `FromEnv` for `DEEPSEEK_API_KEY`, add `HasCredentials` coverage, and add the factory call in `internal/app/session.go`'s `buildProviders`.
