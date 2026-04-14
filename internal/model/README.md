# Package `internal/model`

The `model` package defines the core data structures shared by every layer of FEINO: messages, message parts, tool calls, tool results, and the `Model` interface that all providers implement.

This package has no outward dependencies within the project. It exists to break what would otherwise be import cycles between `provider`, `agent`, `app`, and `tools`.

---

## Message pipeline

```
Message
└── LinkedList[MessagePart]
    ├── TextMessagePart    — streaming token content
    ├── ThoughtPart        — internal reasoning (shown in thinking pane)
    ├── ToolCallPart       — model requests a tool execution
    └── ToolResultPart     — result of a completed tool call
```

---

## Message

```go
type MessageRole string

const (
    MessageRoleUser      MessageRole = "user"
    MessageRoleAssistant MessageRole = "assistant"
    MessageRoleSystem    MessageRole = "system"
    MessageRoleTool      MessageRole = "tool"
)

type Message interface {
    GetID() string
    GetRole() MessageRole
    GetParts() *structs.LinkedList[MessagePart]
    GetTextContent() string         // concatenates all TextMessagePart content
    GetTimestamp() string           // RFC3339
    GetMetadata() map[string]string
}
```

### Construction

```go
msg := model.NewMessage(
    model.WithRole(model.MessageRoleUser),
    model.WithContent(parts),        // *structs.LinkedList[MessagePart]
    model.WithMetadata(map[string]string{"source": "web"}),
)
// ID and timestamp are auto-generated when not provided.
```

---

## MessagePart

```go
type MessagePart interface {
    GetRole() MessageRole
    GetContent() string
    GetTimestamp() string
    GetMetadata() map[string]string
}
```

### Constructors

```go
// Plain text from the model.
part := model.NewTextMessagePart(role, content)

// Model is requesting a tool call.
part := model.NewToolCallPart(role, model.ToolCall{
    ID:        "call_abc123",
    Name:      "file_read",
    Arguments: `{"path":"/tmp/foo"}`,
})

// Result of a tool call.
part := model.NewToolResultPart(role, model.ToolResult{
    CallID:  "call_abc123",
    Name:    "file_read",
    Content: "file contents...",
    IsError: false,
})

// Internal reasoning step (shown in TUI thinking pane, not sent to next turn).
part := model.NewThoughtPart(role, content)
```

---

## Model interface

```go
type Model interface {
    // Metadata
    GetID() string
    GetName() string
    GetDescription() string
    GetHomepage() string
    GetLogger() *slog.Logger

    // Capabilities
    GetContextWindow() int
    GetMaxOutputTokens() int
    SupportsTools() bool

    // Inference
    Infer(
        ctx context.Context,
        history []Message,
        opts InferOptions,
        onPart func(MessagePart),
    ) (Message, Usage, error)
}
```

`onPart` is called for each streaming token as it arrives, enabling real-time display. The returned `Message` contains the complete assembled response.

### InferOptions

```go
type InferOptions struct {
    Config GenerationConfig
    Tools  []Tool
}

type GenerationConfig struct {
    Temperature     *float64
    TopP            *float64
    MaxOutputTokens *int
    StopSequences   []string
}
```

All generation config fields are optional pointers; nil means "use the model's default."

---

## Tool interface

```go
type Tool interface {
    GetName() string
    GetDescription() string
    GetParameters() map[string]any  // JSON Schema
}
```

This is a read-only subset of `tools.Tool`, used by providers to build function-calling schemas without importing `internal/tools`. This deliberate separation prevents an import cycle.

---

## Usage

```go
type Usage struct {
    PromptTokens           int
    CompletionTokens       int
    TotalTokens            int
    CacheCreationTokens    int  // Anthropic prompt caching
    CacheReadTokens        int  // Anthropic prompt caching
    Duration               time.Duration
}
```

---

## Best practices

- **Never cast `MessagePart` directly.** Use type assertions defensively: `if tp, ok := part.(*model.ToolCallPart); ok { ... }`. Parts arrive in a linked list and the type is determined by the provider.
- **Use `GetTextContent()`** to extract all text from a message in one call. Do not manually iterate parts for text-only use cases.
- **`ThoughtPart` is for display only.** Do not append thought parts to the history that is sent to the next inference call. The TUI renders them in the thinking pane and the session layer filters them out before the next `Infer` call.
- **`ToolCall.Arguments` is a raw JSON string**, not a parsed map. Unmarshal it in the tool dispatcher, not in the provider.

---

## Extending

### Adding a new part type

1. Define a struct implementing `MessagePart`.
2. Add a constructor `NewMyPart(...)`.
3. Add a `case *model.MyPart:` branch wherever parts are dispatched (TUI renderer, web event mapper, session verify phase).

### Adding a capability flag

Add a method to the `Model` interface (e.g., `SupportsCaching() bool`) and implement it in every provider. Default to `false` in the `internal/provider/provider.go` base struct if all providers should opt-in.

---

## File map

| File | Responsibility |
|------|---------------|
| `model.go` | `Usage`, `GenerationConfig`, `InferOptions`, `Tool`, `Model` interfaces |
| `message.go` | `MessageRole`, `ToolCall`, `ToolResult`, `MessagePart`, `Message`, `NewMessage` |
| `part.go` | `TextMessagePart`, `ThoughtPart`, `ToolCallPart`, `ToolResultPart` |
