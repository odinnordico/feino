# Package `internal/tokens`

The `tokens` package provides offline token estimation and per-session usage accounting. It is used by the TACOS router to classify request complexity and by the context manager to enforce the system-prompt budget.

---

## Components

| File | Responsibility |
|------|---------------|
| `estimator.go` | `TiktokenEstimator` — offline BPE-based token counting |
| `manager.go` | `UsageManager` — cumulative usage accounting with live listeners |

---

## TiktokenEstimator

Counts tokens offline using the same BPE encoding as the target model. No provider API call is made.

### Construction

```go
est := tokens.NewEstimator(logger,
    tokens.WithChatMLOverheads(4, 3), // perMessage=4, assistantPrime=3
)
```

`WithChatMLOverheads` adds the framing overhead used by OpenAI's ChatML format (4 tokens per message, 3 extra for the assistant primer). Omit for providers that don't use ChatML framing.

### Usage

```go
// Count tokens in a plain string.
n, err := est.EstimateString("Hello, world!", "gpt-4o")

// Count tokens in a single message (text parts + overhead).
n, err := est.EstimateMessage(msg, "claude-opus-4-7")

// Count tokens in a full conversation history.
n, err := est.EstimateMessages(history, "gpt-4o")
```

### Model-to-encoding mapping

| Pattern | Encoding |
|---------|---------|
| `gpt-4*`, `gpt-3.5*` | `cl100k_base` |
| `claude-*` | `cl100k_base` (approximation) |
| `gemini-*` | `cl100k_base` (approximation) |
| Unknown models | `cl100k_base` fallback |

Encoding objects are cached internally after first load. Cache misses are coalesced (singleflight) so concurrent calls for the same model load the encoding only once.

### Estimator interface

```go
type Estimator interface {
    EstimateString(text, modelName string) (int, error)
    EstimateMessage(msg model.Message, modelName string) (int, error)
    EstimateMessages(msgs []model.Message, modelName string) (int, error)
}
```

---

## UsageManager

Accumulates actual token usage reported by providers and notifies listeners.

```go
um := tokens.NewUsageManager(logger)

// Register a listener (called in a goroutine per update).
um.Subscribe(func(meta tokens.UsageMetadata) {
    fmt.Printf("total tokens: %d\n", meta.TotalTokens)
})

// Record a pre-flight estimate before sending to the model.
um.RecordEstimation(estimatedPromptTokens)

// Record actual usage after the model responds.
um.RecordActual(usage)

// Query the running total at any time.
total := um.GetTotal()
```

### UsageMetadata

```go
type UsageMetadata struct {
    model.Usage
    EstimatedPromptTokens int
    Timestamp             time.Time
}
```

`TotalTokens` is computed as `PromptTokens + CompletionTokens` on every `RecordActual` call; the provider's reported `TotalTokens` is not trusted directly.

---

## Best practices

- **Always use `"gpt-4"` as the model name for complexity classification.** The TACOS router uses `EstimateMessages(history, "gpt-4")` as a provider-neutral approximation for bucketing. The result is not used for billing.
- **Pre-flight estimation is approximate.** The actual token count from the provider may differ (e.g., due to special tokens, image tokens, tool schemas). Use `RecordEstimation` only for budget gating, not billing.
- **Listeners run in goroutines.** If a listener updates shared state, it must synchronise with a mutex or channel.
- **Do not call `EstimateMessages` in a hot path without caching.** BPE tokenisation is fast but not free. Cache estimates for unchanged history segments.

---

## Extending

### Supporting a new model encoding

Add a new case to the encoding-selection function in `estimator.go`:

```go
case strings.HasPrefix(model, "deepseek-"):
    return "cl100k_base", nil
```

If the model uses a genuinely different vocabulary (e.g., a SentencePiece-based model), integrate the corresponding tokenizer library and return its count.

### Streaming usage updates to the web UI

The web metrics hub subscribes to `UsageManager` via the `UsageListener` callback. To add a new metric field:

1. Add the field to `UsageMetadata`.
2. Populate it in `RecordActual` or `RecordEstimation`.
3. Update `internal/web/metrics_hub.go` to forward the new field to the gRPC stream.
