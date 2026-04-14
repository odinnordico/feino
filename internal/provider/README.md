# Package `internal/provider`

The `provider` package defines the `Provider` interface and the resilience primitives (circuit breaker, retry with exponential backoff) shared by all LLM integrations. Each provider lives in its own sub-package and wires these primitives around its SDK client.

---

## Provider interface

```go
type Provider interface {
    // Identity
    GetName() string
    GetID() string
    GetDescription() string
    GetHomepage() string
    GetLogger() *slog.Logger

    // Model management
    GetModels(ctx context.Context) ([]model.Model, error)
    SetModel(m model.Model)
    GetSelectedModel() model.Model

    // Observability
    GetCircuitBreaker() *CircuitBreaker
    GetMetrics() *ProviderMetrics
}
```

`GetCircuitBreaker()` is used by the TACOS router to skip providers with open circuits and to penalise half-open circuits during scoring.

### ProviderMetrics

```go
type ProviderMetrics struct {
    TotalRequests uint64  // atomic
    SuccessCount  uint64  // atomic
    FailureCount  uint64  // atomic
}
```

---

## Circuit breaker

```go
cb := provider.NewCircuitBreaker(5, 30*time.Second, logger)
// or
cb := provider.DefaultCircuitBreaker(logger)  // 5 failures, 30s cooldown
```

### State machine

```
Closed ──(5 failures)──► Open ──(30s cooldown)──► HalfOpen ──(success)──► Closed
                                                      │
                                                      └──(failure)──► Open
```

```go
if cb.AllowRequest() {
    result, err := doInference(...)
    if err != nil {
        cb.RecordFailure()
    } else {
        cb.RecordSuccess()
    }
}

state := cb.State() // CircuitClosed | CircuitOpen | CircuitHalfOpen
```

---

## Retry with exponential backoff

```go
result, err := provider.Retry(ctx, retryConfig, circuitBreaker, logger, renewClientFn,
    func(ctx context.Context) (MyResultType, error) {
        return mySDKCall(ctx)
    },
)
```

### RetryConfig defaults

```go
cfg := provider.DefaultRetryConfig()
// MaxRetries: 3
// TotalTimeout: 30s
// InitialDelay: 500ms
// MaxDelay: 4s
```

Backoff formula: `delay = min(initial × 2^attempt, maxDelay) − jitter(25%)`.

### Error classification

```go
provider.IsRetryable(err)      // network errors, DNS, 5xx, 429, rate limits
provider.NeedsClientRenewal(err) // 401, 403, expired credentials
```

When `NeedsClientRenewal` returns true, the `Retry` loop calls `renewClientFn` before the next attempt. This allows the provider to re-read updated API keys from the environment or keyring without restarting the process.

---

## Sub-packages

| Package | Provider | Key feature |
|---------|---------|-------------|
| `anthropic/` | Anthropic Claude | Prompt caching, streaming SSE, auto-pagination |
| `openai/` | OpenAI | ChatML framing, tool/function calling |
| `gemini/` | Google Gemini | API key + Vertex AI (ADC) auth, both streaming |
| `ollama/` | Ollama (local) | Spawns daemon on first use, extended cold-start timeouts |
| `openaicompat/` | Any OpenAI-compatible server | Optional auth, configurable base URL |

### OpenAI-compatible servers

```yaml
providers:
  openai_compat:
    base_url: http://localhost:8000/v1
    api_key: ""          # omit or leave empty for unauthenticated servers
    name: vLLM
    default_model: mistralai/Mistral-7B-Instruct-v0.2
```

| Server | Typical base URL |
|--------|-----------------|
| vLLM | `http://localhost:8000/v1` |
| LocalAI | `http://localhost:8080/v1` |
| LM Studio | `http://localhost:1234/v1` |
| Llamafile | `http://localhost:8080/v1` |

---

## Adding a new provider

1. **Create the package** — `internal/provider/myprovider/`

2. **Implement `Provider` interface:**

```go
type Provider struct {
    client    *mysdk.Client
    cb        *provider.CircuitBreaker
    metrics   *provider.ProviderMetrics
    retryConf provider.RetryConfig
    logger    *slog.Logger
    selected  model.Model
}

func NewProvider(ctx context.Context, apiKey string, logger *slog.Logger) (*Provider, error) {
    p := &Provider{
        client:    mysdk.New(apiKey),
        cb:        provider.DefaultCircuitBreaker(logger),
        metrics:   &provider.ProviderMetrics{},
        retryConf: provider.DefaultRetryConfig(),
        logger:    logger,
    }
    return p, nil
}

func (p *Provider) GetModels(ctx context.Context) ([]model.Model, error) {
    return provider.Retry(ctx, p.retryConf, p.cb, p.logger, p.renewClient,
        func(ctx context.Context) ([]model.Model, error) {
            // call p.client.ListModels(ctx)
        },
    )
}
```

3. **Implement `model.Model` interface** for each model ID, including streaming in `Infer`.

4. **Implement `renewClient`** to re-initialise the SDK client from the current environment/keyring.

5. **Add to `session.go`** — add a factory call in `buildProviders` keyed on `cfg.Providers.MyProvider.APIKey != ""`.

6. **Add tests** using `httptest.NewServer` to mock the provider's API. See `anthropic/anthropic_test.go` for a streaming SSE mock pattern.

---

## Testing pattern

Never use real API keys in tests. Mock the HTTP server:

```go
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/event-stream")
    fmt.Fprintf(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hello\"}}\n\n")
    fmt.Fprintf(w, "data: {\"type\":\"message_stop\"}\n\n")
}))
defer srv.Close()

p, _ := anthropic.NewProvider(ctx, "test-key", srv.URL, logger)
```

---

## Best practices

- **Always wrap SDK calls in `Retry`.** Network calls fail transiently; the retry layer handles backoff, circuit breaking, and client renewal transparently.
- **Record `RecordSuccess`/`RecordFailure` on the circuit breaker** after every SDK call, not just top-level `Infer` calls. This includes `GetModels` calls.
- **Implement streaming in `Infer`.** The session layer expects `onPart` to be called for each token. Buffering the full response and emitting it at once degrades perceived latency significantly.
- **Never store API keys in the `Provider` struct.** Read them from the environment or keyring inside `renewClient` so key rotation takes effect on the next retry without restarting.
