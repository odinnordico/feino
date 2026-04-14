# Package `internal/mcp`

The `mcp` package provides a typed client for connecting to **Model Context Protocol** servers. MCP servers expose tools, resources, and prompt templates over a standard protocol; this package bridges them into the FEINO tool pipeline so the agent can invoke remote capabilities without any changes to the core machinery.

---

## Overview

Two transports are supported:

| Transport | Use case |
|-----------|---------|
| **stdio** | Spawn a local subprocess. The client communicates over the process's stdin/stdout using newline-delimited JSON. |
| **SSE** | Connect to a remote endpoint over streamable HTTP with server-sent events. |

Both transports produce a `Session` with the same API. The agent pipeline receives a `[]tools.Tool` slice from `Session.AsTools` and treats MCP tools identically to native tools.

---

## Client construction

```go
client := mcp.NewClient("feino", "1.0.0",
    mcp.WithClientLogger(logger),
    mcp.WithToolListChangedHandler(func(ctx context.Context, req *sdkmcp.ToolListChangedRequest) {
        // refresh tool list
    }),
)
```

---

## Connecting to a server

### Stdio (local subprocess)

```go
session, err := client.ConnectStdio(ctx, mcp.StdioClientConfig{
    Command: "/usr/local/bin/my-mcp-server",
    Args:    []string{"--port", "0"},
    Env:     []string{"LOG_LEVEL=debug"},
    TerminateDuration: 5 * time.Second,
})
if err != nil {
    return err
}
defer session.Close()
```

Shutdown sequence on `Close`: stdin closed → SIGTERM after `TerminateDuration` → SIGKILL if still running.

### SSE (remote server)

```go
session, err := client.ConnectSSE(ctx, mcp.SSEClientConfig{
    Endpoint:   "https://my-mcp-server.example.com/mcp",
    HTTPClient: myCustomHTTPClient, // nil = http.DefaultClient
    MaxRetries: 3,
})
```

---

## Session API

```go
// List all tools the server advertises (handles pagination automatically).
tools, err := session.ListTools(ctx)

// Call a tool by name with JSON-serializable arguments.
result, err := session.CallTool(ctx, "search", map[string]any{"query": "feino"})

// List resources.
resources, err := session.ListResources(ctx)

// Read a resource by URI.
content, err := session.ReadResource(ctx, "file:///data/schema.sql")

// List prompt templates.
prompts, err := session.ListPrompts(ctx)

// Retrieve a prompt with template substitution.
rendered, err := session.GetPrompt(ctx, "summarise", map[string]string{"lang": "en"})

// Server metadata from the initialise handshake.
info := session.ServerInfo()

// Close the session and terminate the underlying transport.
session.Close()
```

---

## Bridging to the tool pipeline

```go
// Wrap every server tool as a tools.Tool.
feinoTools, err := session.AsTools(ctx)
if err != nil {
    return err
}

// Wire into the session.
sess, err := app.New(cfg, app.WithExtraTools(feinoTools))
```

`AsTools` returns a `[]tools.Tool` where each element delegates `Run` to `CallTool`. The tool name, description, and JSON Schema input definition come directly from the MCP server's tool manifest.

To bridge a single named tool:

```go
t, err := session.AsTool(ctx, "web_search")
```

---

## Security considerations

MCP tools are dynamically discovered and are not self-classified (they do not implement `tools.Classified`). The `SecurityGate` defaults them to `PermissionDangerZone` unless an explicit override is provided via `WithExtraToolLevels`:

```go
gate := security.NewSecurityGate(security.PermissionBash,
    security.WithExtraToolLevels(map[string]security.PermissionLevel{
        "web_search": security.PermissionRead,
        "file_read":  security.PermissionRead,
    }),
)
```

Always review the tool list returned by `ListTools` before wiring an MCP server into a session.

---

## Best practices

- **Close sessions promptly.** Stdio sessions hold an OS process alive; SSE sessions hold an HTTP connection. Defer `session.Close()` immediately after connecting.
- **Handle `ToolListChangedNotification`.** Server tool lists can change at runtime. Register `WithToolListChangedHandler` to call `session.AsTools` again and update the agent's tool set.
- **Set `MaxRetries` for SSE.** Network blips will disconnect SSE streams. The client reconnects automatically up to `MaxRetries` times.
- **Validate tool names.** MCP tool names come from an external server. Ensure they do not conflict with native tool names before merging the two slices.

---

## Extending

### Connecting to a new transport

The `Client.Connect` method accepts any `sdkmcp.Transport`. To add a custom transport (e.g., Unix socket, gRPC):

```go
myTransport := &myCustomTransport{...}
session, err := client.Connect(ctx, myTransport)
```

### Wrapping MCP resources as context

Resources are not automatically injected into the system prompt. To include them:

1. Call `session.ListResources(ctx)` and `session.ReadResource(ctx, uri)`.
2. Convert the content into `context.SemanticChunk` values.
3. Call `contextManager.AddCodeContext(chunks)`.

---

## File map

| File | Responsibility |
|------|---------------|
| `client.go` | `Client`, `Session`, transport configuration, all session operations |
| `bridge.go` | `mcpToolAdapter` — adapts MCP tools to `tools.Tool`, `AsTools`, `AsTool` |
| `client_test.go` | Unit tests using in-memory transports |
