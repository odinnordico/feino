# cmd/feino

Binary entry point for the FEINO AI agent CLI. Parses flags, loads configuration, and routes to one of three execution modes.

---

## Execution modes

| Mode | Flag | Description |
|------|------|-------------|
| **TUI** | _(default)_ | Full terminal UI (Bubble Tea). Requires a real terminal. |
| **REPL** | `--no-tui` | Plain stdin/stdout read-eval-print loop. Useful for scripting and headless environments. |
| **Web** | `--web` | HTTP/2 server with Connect RPC API and embedded React SPA. |

---

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--no-tui` | `false` | Use plain REPL instead of TUI |
| `--web` | `false` | Start web server |
| `--web-host` | `127.0.0.1` | Bind address for web server |
| `--web-port` | `7700` | Port for web server |
| `--log-level` | `""` | Override config log level: `debug`, `info`, `warn`, `error` |

---

## Startup sequence

1. Parse flags.
2. Load `~/.feino/config.yaml` (missing file is not an error).
3. Overlay environment variables via `config.FromEnv()`.
4. Apply `--log-level` override if provided.
5. Install signal handler for `SIGINT` / `SIGTERM` → context cancellation.
6. Route to the selected mode.

---

## Signal handling

The binary installs a `signal.NotifyContext` for `os.Interrupt` (Ctrl+C) and `syscall.SIGTERM`. Cancelling the context triggers graceful shutdown in all three modes:

- **TUI** — Bubble Tea catches the signal and initiates clean exit.
- **REPL** — `repl.Run` returns when the context is cancelled.
- **Web** — The server calls `Shutdown(ctx)` with a 5-second timeout.

`context.Canceled` and `context.DeadlineExceeded` errors are treated as clean exits (not logged as errors).

---

## First-run wizard

When no credentials are configured (`config.HasCredentials(cfg) == false`), the TUI mode automatically launches the setup wizard before showing the chat view. The wizard is not triggered in REPL or web modes.

To force the wizard, delete `~/.feino/config.yaml`:

```bash
rm ~/.feino/config.yaml && feino
```

---

## CORS

CORS headers are enabled automatically when `--web-host` is set to a non-loopback address. Loopback addresses (`localhost`, `127.x.x.x`, `::1`) do not trigger CORS.

---

## Build tags

The web mode requires the `web` build tag to embed the React SPA:

```bash
# Development (SPA served separately by Vite on :5173)
go build ./cmd/feino

# Production (SPA embedded in binary)
cd internal/web/ui && npm run build
go build -tags web ./cmd/feino
```

Without the tag, `--web` starts the API server but serves a placeholder page instead of the React app.
