# Package `internal/app`

The `app` package is the UI-agnostic orchestration core of FEINO. It wires every subsystem together — providers, context assembly, security, tools, memory — into a single `Session` type that any front-end (TUI, web, REPL) drives identically.

The design contract is simple: call `Send`, receive `Event`s via `Subscribe`. The session never knows or cares what renders the output.

---

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                        Session                               │
│                                                              │
│  ┌──────────────┐   ┌──────────────┐   ┌─────────────────┐  │
│  │ContextManager│   │ TACOSRouter  │   │  SecurityGate   │  │
│  │  (Gather)    │   │  (Act)       │   │  (Verify)       │  │
│  └──────────────┘   └──────────────┘   └─────────────────┘  │
│          │                  │                   │            │
│          └──────────────────┴───────────────────┘            │
│                             │                                │
│                    ┌────────▼────────┐                       │
│                    │  StateMachine   │                       │
│                    │  Gather→Act→    │                       │
│                    │  Verify→loop   │                       │
│                    └─────────────────┘                       │
│                             │                                │
│                    ┌────────▼────────┐                       │
│                    │  EventHandler   │ ← TUI / Web / REPL   │
│                    └─────────────────┘                       │
└──────────────────────────────────────────────────────────────┘
```

### ReAct loop phases

| Phase | Responsibility |
|-------|---------------|
| **Gather** | Assemble the system prompt via `ContextManager.AssembleContext`. Loads skills, injects memories, respects `MaxBudget`. |
| **Act** | `TACOSRouter.SelectOptimalModel` picks the best provider/model. The model streams tokens; each `MessagePart` is emitted as `EventPartReceived`. |
| **Verify** | Inspect the assistant's reply. If it contains tool calls, dispatch each through `SecurityGate`, append results to history, reset the retry counter, and signal the machine to loop back to Act. If no tool calls, signal `StateComplete`. |

The retry counter increments only on genuine Verify failures (not on successful tool dispatches). The machine transitions to `StateFailed` after `MaxRetries` consecutive failures.

---

## API Reference

### Events

```go
type EventKind string

const (
    EventPartReceived EventKind = "part_received"   // payload: model.MessagePart
    EventStateChanged EventKind = "state_changed"   // payload: agent.ReActState
    EventUsageUpdated EventKind = "usage_updated"   // payload: tokens.UsageMetadata
    EventError        EventKind = "error"           // payload: error
    EventComplete     EventKind = "complete"        // payload: model.Message
)
```

### Construction

```go
sess, err := app.New(cfg, app.WithLogger(logger))
```

Available `SessionOption`s:

| Option | Purpose |
|--------|---------|
| `WithLogger(l)` | Replace the default `slog.Default()` |
| `WithHistory(msgs)` | Seed the conversation with prior messages |
| `WithProviders(ps)` | Inject custom providers (skips auto-detection) |
| `WithMemoryStore(s)` | Attach a `memory.Store` for persistent facts |
| `WithExtraTools(ts)` | Append extra tools beyond the native suite |

### Session methods

```go
// Register an event listener. Safe to call before or after Send.
func (s *Session) Subscribe(h EventHandler)

// Append a user message and start the ReAct loop asynchronously.
// Returns ErrBusy if a turn is already running.
func (s *Session) Send(ctx context.Context, text string) error

// Abort the current in-flight turn.
func (s *Session) Cancel()

// Return a snapshot of conversation history.
func (s *Session) History() []model.Message

// Clear history and reset the state machine.
func (s *Session) Reset() error

// Query the current ReAct phase.
func (s *Session) GetCurrentState() agent.ReActState

// Return the active configuration.
func (s *Session) Config() config.Config

// Hot-swap security/context settings without rebuilding providers.
func (s *Session) UpdateConfig(cfg config.Config) error

// Rescan the plugins directory and hot-swap the tool set.
func (s *Session) ReloadPlugins() (int, error)

// Register an interactive callback for permission escalation prompts.
func (s *Session) SetPermissionCallback(fn func(toolName, description string, required, allowed int) bool)

// Timed or session-wide bypass of permission checks.
func (s *Session) SetBypassMode(until time.Time)
func (s *Session) ClearBypassMode()
func (s *Session) IsBypassActive() bool
```

---

## Security model

Tool calls are dispatched through a three-layer `SecurityGate`:

1. **Permission level** — the tool's declared level must not exceed the configured `PermissionLevel` (`read ≤ write ≤ bash ≤ danger_zone`).
2. **Path policy** — file tools whose target path is outside `AllowedPaths` are denied with `ErrPathDenied`.
3. **AST blacklist** — `shell_exec` commands are parsed as Bash ASTs; network tools and destructive filesystem operations are blocked regardless of permission level.

When a tool is denied, the registered `permissionCallback` is invoked. If the user approves, the gate is bypassed for that single invocation. `SetBypassMode` creates a time-bounded window where all checks are auto-approved.

---

## Best practices

- **Never share a Session across goroutines.** `Send` starts its own goroutine; all concurrent access to history is serialised internally.
- **Always register at least one `Subscribe` handler before calling `Send`**, otherwise `EventComplete` fires unobserved and the caller never knows the turn finished.
- **Use `ErrBusy`** — check `errors.Is(err, app.ErrBusy)` after `Send` to detect overlapping turns cleanly.
- **Config hot-swap via `UpdateConfig`** for security and budget changes. Do not reconstruct the session; providers are expensive to initialise.
- **`Reset` before re-using** a completed session for a fresh conversation. It clears history and returns the state machine to `StateInit`.

---

## Extending the session

### Adding a tool

1. Create a `tools.Tool` implementation (or use `tools.NewTool`).
2. Pass it via `WithExtraTools` at construction, or drop an executable + JSON manifest in the `PluginsDir` and call `ReloadPlugins`.
3. No changes to `Session` are required.

### Adding a provider

1. Implement `provider.Provider` in a new sub-package under `internal/provider/`.
2. Inject it with `WithProviders`. If you want it picked up by auto-detection, add the factory call to the `buildProviders` private function in `session.go`.

### Customising the ReAct loop

The three phase handlers (`gatherFn`, `actFn`, `verifyFn`) are closures set up inside `runReActLoop`. To insert a new phase:

1. Add a new `PhaseHandler` closure.
2. Modify `sm.SetHandlers(...)` to include it, adjusting the state machine's `validTransitions` map in `internal/agent/machine.go` accordingly.
3. Add `OnBefore`/`OnAfter` hooks for observability without modifying the phase logic.
