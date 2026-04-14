# Package `internal/web`

The `web` package implements FEINO's browser-based UI: an HTTP/2 server that exposes a Connect RPC API and serves an embedded React SPA. It is activated with the `--web` flag and the `web` build tag.

---

## Architecture

```
Browser
  │  HTTP/2 h2c (cleartext) or HTTPS
  ▼
┌──────────────────────────────────────────────────┐
│                  web.Server                      │
│                                                  │
│  ┌─────────────────┐  ┌────────────────────────┐ │
│  │  Connect RPC    │  │  React SPA (embedded)  │ │
│  │  /feino.v1.*    │  │  /  (index.html fallbk)│ │
│  └────────┬────────┘  └────────────────────────┘ │
│           │                                      │
│  ┌────────▼────────┐                             │
│  │  FeinoHandler   │                             │
│  │  (handler.go)   │                             │
│  └────────┬────────┘                             │
│           │                                      │
│  ┌────────▼──────────────────────┐               │
│  │  app.Session (build_session)  │               │
│  └───────────────────────────────┘               │
└──────────────────────────────────────────────────┘
```

---

## Server

```go
srv, err := web.NewServer(web.Options{
    Host:       "localhost",
    Port:       8080,
    Logger:     logger,
    ConfigPath: configPath,
})

// Run blocks until ctx is cancelled, then gracefully shuts down with a 5s timeout.
err = srv.Run(ctx)
```

### CORS

CORS headers are added automatically when the bind host is non-loopback (i.e., `--web-host` is set to a routable interface). Local `localhost`/`127.0.0.1` binds do not add CORS headers.

---

## Connect RPC API

The full service definition is in `gen/feino/v1/feino.proto`. Key methods:

| Method | Type | Description |
|--------|------|-------------|
| `GetSessionState` | Unary | Current ReAct state and busy flag |
| `SendMessage` | Server-streaming | Send a user message; streams `AgentEvent`s as the turn progresses |
| `CancelTurn` | Unary | Abort in-flight turn |
| `ResolvePermission` | Unary | Approve or deny a pending tool permission request |
| `GetHistory` | Unary | Full conversation history as protobuf messages |
| `ResetSession` | Unary | Clear history and reset state machine |
| `GetConfig` | Unary | Return current config as protobuf |
| `UpdateConfig` | Unary | Hot-swap config |
| `GetCredentials` | Unary | List credential keys for a service |
| `SetCredentials` | Unary | Store a credential |
| `ListMemory` | Unary | List or search memory entries |
| `WriteMemory` | Unary | Create a new memory entry |
| `UpdateMemory` | Unary | Update an existing entry |
| `DeleteMemory` | Unary | Delete an entry |
| `StreamMetrics` | Server-streaming | Live token usage and latency metrics |

---

## Key files

| File | Responsibility |
|------|---------------|
| `server.go` | HTTP server, h2c, Connect RPC mux, SPA mux |
| `handler.go` | `FeinoServiceHandler` — all RPC method implementations |
| `build_session.go` | Construct `app.Session` with all dependencies for the web context |
| `session_manager.go` | Multiplex events from one session to multiple concurrent SSE clients |
| `metrics_hub.go` | Aggregate token usage across clients; stream to `StreamMetrics` subscribers |
| `event_mapper.go` | `app.Event` → `feinov1.AgentEvent` protobuf conversion |
| `history_mapper.go` | `model.Message` ↔ `feinov1.Message` protobuf conversion |
| `config_mapper.go` | `config.Config` ↔ `feinov1.Config` protobuf conversion |
| `file_service.go` | Sandboxed filesystem access for uploads and `@path` resolution |
| `atref.go` | Expand `@path` tokens in user messages to `<file>…</file>` XML |
| `spa_handler.go` | Serve embedded React SPA with `index.html` fallback for client-side routing |
| `embed.go` | `//go:embed dist` — includes the built SPA; only compiled with `-tags web` |
| `embed_stub.go` | Stub for builds without `-tags web` |

---

## Permission resolution flow

When the agent's security gate needs user approval, it:

1. Creates a unique request ID (`sessionID:toolName:sequenceNumber`).
2. Stores a blocking channel in `sessionManager.pendingPermissions`.
3. Pushes an `AgentEvent` with kind `permission_request` to all connected clients.
4. Blocks until the client calls `ResolvePermission` with the request ID.
5. The channel receives `true` (approve) or `false` (deny), unblocking the gate.

---

## File service

`FileService` provides a sandboxed view of the filesystem, restricted to the session's working directory:

```go
// Upload a file; returns a stable token for subsequent resolution.
token, err := fs.Upload(filename, reader)

// Resolve a token back to the file path (within the working directory).
path, err := fs.Resolve(token)
```

Upload tokens are random UUIDs. The service maintains a `sync.RWMutex`-protected map of tokens to paths, and a `Close` method that removes all uploaded temp files.

---

## Build tags

The SPA is only embedded when building with `-tags web`:

```bash
go build -tags web ./cmd/feino
```

Without the tag, the server serves a placeholder page. The Vite dev server (port 5173) proxies API calls to the Go server (port 8080) during development.

---

## Best practices

- **Stream events immediately.** The `SendMessage` handler flushes each `AgentEvent` as it arrives via `flush()`. Do not buffer events — the user's perceived latency depends on first-token time.
- **All proto conversions go through mappers.** Never convert `config.Config` ↔ proto directly in the handler; always use `config_mapper.go`. This keeps the handler thin and the mappings testable.
- **Permission channels must be closed on session reset.** A stale `pendingPermissions` channel that is never resolved causes the agent goroutine to leak. `ResetSession` drains and closes all pending channels.
- **`FileService.Close` must be called on server shutdown.** The server's `defer assets.fileSvc.Close()` handles this. Forgetting it leaks temp files.
- **Test the handler with `httptest.NewServer`.** The Connect RPC handler is a standard `http.Handler`; integration tests can use a real server without binding to a port.

---

## Extending

### Adding a new RPC method

1. Add the method to `gen/feino/v1/feino.proto` and regenerate with `buf generate`.
2. Implement the handler method on `FeinoServiceHandler` in `handler.go`.
3. Add a converter function to the appropriate mapper file if new types are involved.
4. Add a test in `handler_test.go`.
5. Add the React client call in the frontend `src/client.ts`.

### Adding a new config field

1. Add the field to the protobuf schema, regenerate, update `config_mapper.go`.
2. Update the React `SettingsPanel` to render the new field.
3. The mapper functions are the only place that knows about both the Go and proto schemas; keep the mapping logic there.
