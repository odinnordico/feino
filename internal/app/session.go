// Package app provides the UI-agnostic application session that wires all
// FEINO subsystems together. Any user interface — REPL, web handler, or desktop
// binding — drives the session through the Send method and receives results via
// registered EventHandlers.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/odinnordico/feino/internal/agent"
	"github.com/odinnordico/feino/internal/config"
	appctx "github.com/odinnordico/feino/internal/context"
	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/provider"
	anthropicprov "github.com/odinnordico/feino/internal/provider/anthropic"
	geminiprov "github.com/odinnordico/feino/internal/provider/gemini"
	ollamaprov "github.com/odinnordico/feino/internal/provider/ollama"
	openaiprov "github.com/odinnordico/feino/internal/provider/openai"
	openaicompatprov "github.com/odinnordico/feino/internal/provider/openaicompat"
	"github.com/odinnordico/feino/internal/security"
	"github.com/odinnordico/feino/internal/structs"
	"github.com/odinnordico/feino/internal/tokens"
	"github.com/odinnordico/feino/internal/tools"
)

// ErrBusy is returned by Send when another turn is already in progress.
var ErrBusy = errors.New("app: session is busy processing a previous message")

// EventKind identifies the category of a session event.
type EventKind string

const (
	// EventPartReceived fires for each streaming token from the model.
	// Payload: model.MessagePart
	EventPartReceived EventKind = "part_received"

	// EventStateChanged fires on every ReAct state transition.
	// Payload: agent.ReActState
	EventStateChanged EventKind = "state_changed"

	// EventUsageUpdated fires when token accounting changes.
	// Payload: tokens.UsageMetadata
	EventUsageUpdated EventKind = "usage_updated"

	// EventError fires on non-fatal errors during a turn.
	// Payload: error
	EventError EventKind = "error"

	// EventComplete fires when a turn finishes successfully.
	// Payload: model.Message (final assembled assistant message)
	EventComplete EventKind = "complete"
)

// Event carries a single notification from the session to registered handlers.
type Event struct {
	Kind    EventKind
	Payload any
}

// EventHandler is the callback signature for session events. Handlers are
// called synchronously in the goroutine that owns the event — keep them fast.
type EventHandler func(Event)

// Session wires all FEINO subsystems and presents a clean, UI-neutral API.
// Construct one with New; it is safe for concurrent use after construction.
type Session struct {
	mu       sync.RWMutex
	cfg      *config.Config
	logger   *slog.Logger
	inFlight atomic.Bool

	// subsystems
	ctxMgr            appctx.Manager
	security          *security.Gate
	dispatcher        *security.Dispatcher // gate-wrapped; used for normal dispatch
	ungatedDispatcher *security.Dispatcher // unwrapped; used only after explicit user approval
	router            *agent.TACOSRouter
	machine           *agent.StateMachine
	usage             *tokens.UsageManager

	// pre-built tool list for InferOptions — rebuilt by ReloadPlugins
	modelTools []model.Tool
	pluginsDir string // resolved once in New, reused by ReloadPlugins

	// permissionAsk is called when the gate denies a tool call. It blocks until
	// the user approves or denies, then returns the decision. Nil means no
	// interactive prompt — the denial propagates as a tool error as before.
	permissionAsk func(ctx context.Context, toolName string, required, allowed security.PermissionLevel) bool

	// bypass mode — when active all gate denials are auto-approved without
	// prompting. bypassUntil holds the expiry for timed modes; bypassSession
	// means "active until the process exits" (bypassUntil is ignored).
	bypassUntil   time.Time
	bypassSession bool

	// conversation
	history []model.Message
	cancel  context.CancelFunc

	// event delivery
	handlersMu sync.RWMutex
	handlers   []EventHandler

	// for test injection: skip provider construction when non-nil
	prebuiltProviders []provider.Provider

	// extraTools is appended to the native + plugin tool set before the
	// security gate and dispatcher are built.
	extraTools []tools.Tool

	// memoryStore, when non-nil, is forwarded to the context manager so
	// agent-learned memories are injected into every system prompt.
	memoryStore ctxMemoryStore
}

// ctxMemoryStore is the subset of memory.Store the session needs to forward to
// the context manager. Defined locally to avoid importing internal/memory here.
type ctxMemoryStore interface {
	FormatPrompt() (string, error)
}

// SessionOption configures a Session.
type SessionOption func(*Session)

// WithLogger sets the logger for the session and its subsystems.
func WithLogger(l *slog.Logger) SessionOption {
	return func(s *Session) { s.logger = l }
}

// WithHistory pre-loads conversation history into the session.
func WithHistory(h []model.Message) SessionOption {
	return func(s *Session) {
		s.history = make([]model.Message, len(h))
		copy(s.history, h)
	}
}

// WithProviders injects pre-built providers, bypassing normal credential-based
// construction. Intended for tests and embedding scenarios.
func WithProviders(provs ...provider.Provider) SessionOption {
	return func(s *Session) {
		s.prebuiltProviders = provs
	}
}

// WithMemoryStore attaches an agent memory store to the session. The store is
// forwarded to the context manager so memories are injected into every system
// prompt. Construct one with memory.NewFileStore.
func WithMemoryStore(store ctxMemoryStore) SessionOption {
	return func(s *Session) { s.memoryStore = store }
}

// WithExtraTools appends additional pre-built tools to the native tool set.
// They are appended after native tools and script plugins, and are wrapped by
// the security gate exactly like native tools.
func WithExtraTools(t ...tools.Tool) SessionOption {
	return func(s *Session) {
		s.extraTools = append(s.extraTools, t...)
	}
}

// New constructs and wires a Session from cfg. Provider credentials are
// resolved by merging cfg with environment variables (env always wins).
// Returns an error if no LLM provider can be initialised.
func New(cfg *config.Config, opts ...SessionOption) (*Session, error) {
	// Merge env overrides first so all downstream code sees the final config.
	cfg = config.Merge(cfg, config.FromEnv())

	s := &Session{
		cfg:     cfg,
		logger:  slog.Default(),
		history: make([]model.Message, 0),
	}
	for _, opt := range opts {
		opt(s)
	}

	// Resolve working directory.
	workingDir := cfg.Context.WorkingDir
	if workingDir == "" {
		var err error
		workingDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("app: cannot determine working directory: %w", err)
		}
	}

	// Context manager.
	ctxOpts := []appctx.ManagerOption{
		appctx.WithContextLogger(s.logger),
	}
	if cfg.Context.GlobalConfigPath != "" {
		ctxOpts = append(ctxOpts, appctx.WithGlobalConfigPath(cfg.Context.GlobalConfigPath))
	}
	if profile := cfg.User.FormatPrompt(); profile != "" {
		ctxOpts = append(ctxOpts, appctx.WithUserProfile(profile))
	}
	if s.memoryStore != nil {
		ctxOpts = append(ctxOpts, appctx.WithMemoryStore(s.memoryStore))
	}
	s.ctxMgr = appctx.NewFileSystemContextManager(workingDir, ctxOpts...)
	s.ctxMgr.AutoDetect()

	// Security gate.
	permLevel := parsePermissionLevel(cfg.Security.PermissionLevel)
	gateOpts := []security.GateOption{
		security.WithGateLogger(s.logger),
	}
	if len(cfg.Security.AllowedPaths) > 0 {
		pp := security.NewPathPolicy()
		for _, p := range cfg.Security.AllowedPaths {
			if err := pp.Allow(p); err != nil {
				s.logger.Warn("invalid allowed path in config", "path", p, "error", err)
			}
		}
		gateOpts = append(gateOpts, security.WithPathPolicy(pp))
	}
	if cfg.Security.EnableASTBlacklist != nil && *cfg.Security.EnableASTBlacklist {
		gateOpts = append(gateOpts, security.WithASTBlacklist(security.NewASTBlacklist()))
	}
	s.security = security.NewSecurityGate(permLevel, gateOpts...)

	// Native tools — context manager gets unwrapped tools for schema generation;
	// the dispatcher gets gate-wrapped tools so every dispatch is checked.
	// modelTools is the same set converted to model.Tool for InferOptions; it is
	// stateless and safe to reuse across turns, so we build it once here.
	allTools := tools.NewNativeTools(s.logger)

	// Script plugins — loaded from PluginsDir (default ~/.feino/plugins).
	pluginsDir := cfg.Context.PluginsDir
	if pluginsDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			pluginsDir = filepath.Join(home, ".feino", "plugins")
		}
	}
	s.pluginsDir = pluginsDir // persist for ReloadPlugins

	if pluginsDir != "" {
		pluginTools, err := tools.LoadPlugins(pluginsDir, s.logger)
		if err != nil {
			s.logger.Warn("plugin loader error", "dir", pluginsDir, "error", err)
		} else if len(pluginTools) > 0 {
			s.logger.Info("script plugins loaded", "count", len(pluginTools), "dir", pluginsDir)
			allTools = append(allTools, pluginTools...)
		}
	}

	// Inject extra tools (e.g. service integrations registered by the TUI).
	if len(s.extraTools) > 0 {
		s.logger.Info("extra tools registered", "count", len(s.extraTools))
		allTools = append(allTools, s.extraTools...)
	}

	s.ctxMgr.SetTools(allTools)
	s.dispatcher = security.NewDispatcher(s.security.WrapTools(allTools)...)
	s.ungatedDispatcher = security.NewDispatcher(allTools...)
	s.modelTools = makeModelTools(allTools)

	// Token estimator and TACOS router.
	estimator := tokens.NewEstimator(s.logger)
	routerOpts := []agent.TACOSOption{
		agent.WithTACOSLogger(s.logger),
	}
	if cfg.Agent.MetricsPath != "" {
		routerOpts = append(routerOpts, agent.WithPersistencePath(cfg.Agent.MetricsPath))
	}
	s.router = agent.NewTACOSRouter(estimator, routerOpts...)
	if cfg.Agent.HighComplexityThreshold != 0 || cfg.Agent.LowComplexityThreshold != 0 {
		high := cfg.Agent.HighComplexityThreshold
		low := cfg.Agent.LowComplexityThreshold
		if high == 0 {
			high = agent.DefaultHighComplexityThreshold
		}
		if low == 0 {
			low = agent.DefaultLowComplexityThreshold
		}
		s.router.SetComplexityThresholds(low, high)
	}

	// Register providers.
	if len(s.prebuiltProviders) > 0 {
		for _, p := range s.prebuiltProviders {
			s.router.RegisterProvider(p)
		}
	} else {
		provs, err := buildProviders(context.Background(), cfg.Providers, s.logger)
		if err != nil {
			return nil, fmt.Errorf("app: no providers available: %w", err)
		}
		for _, p := range provs {
			s.router.RegisterProvider(p)
		}
	}

	// Usage manager — subscribes to emit EventUsageUpdated.
	s.usage = tokens.NewUsageManager(s.logger)
	s.usage.Subscribe(func(meta tokens.UsageMetadata) {
		s.emit(Event{Kind: EventUsageUpdated, Payload: meta})
	})

	// ReAct state machine — subscribes to emit EventStateChanged.
	s.machine = agent.NewStateMachine()
	if cfg.Agent.MaxRetries != 0 {
		s.machine.SetMaxRetries(cfg.Agent.MaxRetries)
	}
	s.machine.Subscribe(func(state agent.ReActState) {
		s.emit(Event{Kind: EventStateChanged, Payload: state})
	})

	return s, nil
}

// Subscribe registers h to receive all session events. Safe to call
// concurrently with Send.
func (s *Session) Subscribe(h EventHandler) {
	s.handlersMu.Lock()
	defer s.handlersMu.Unlock()
	s.handlers = append(s.handlers, h)
}

// Send appends text as a user message and starts the ReAct loop in a
// background goroutine. It returns immediately. If a turn is already in
// progress, ErrBusy is returned without modifying any state.
//
// On failure the user message and any partial history from the turn are rolled
// back, leaving history in the same state it was before Send was called.
func (s *Session) Send(ctx context.Context, text string) error {
	if !s.inFlight.CompareAndSwap(false, true) {
		return ErrBusy
	}

	userMsg := newTextMessage(model.MessageRoleUser, text)
	runCtx, cancel := context.WithCancel(ctx)

	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()

	go func() {
		defer s.inFlight.Store(false)
		defer cancel()

		// Record the history length before the turn so we can roll back on failure.
		s.mu.Lock()
		preTurnLen := len(s.history)
		s.history = append(s.history, userMsg)
		s.mu.Unlock()

		if err := s.runReActLoop(runCtx); err != nil {
			s.mu.Lock()
			s.history = s.history[:preTurnLen]
			s.mu.Unlock()
			s.emit(Event{Kind: EventError, Payload: err})
		}
	}()
	return nil
}

// Cancel aborts the current in-flight Send. It is a no-op when no turn is
// in progress.
func (s *Session) Cancel() {
	s.mu.RLock()
	cancel := s.cancel
	s.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

// History returns a snapshot of the conversation history. Safe to call at
// any time, including while a turn is in progress.
func (s *Session) History() []model.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := make([]model.Message, len(s.history))
	copy(snap, s.history)
	return snap
}

// Reset clears conversation history and resets the ReAct state machine.
// Returns ErrBusy if a Send is in flight — call Cancel first.
func (s *Session) Reset() error {
	if s.inFlight.Load() {
		return ErrBusy
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = make([]model.Message, 0)
	s.machine.Reset()
	return nil
}

// GetCurrentState returns the ReAct state the session is currently in.
// Returns StateInit when no turn is in progress.
func (s *Session) GetCurrentState() agent.ReActState {
	return s.machine.GetState()
}

// Config returns a copy of the active configuration.
func (s *Session) Config() *config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// UpdateConfig replaces the active configuration. Security gate and context
// budget settings take effect on the next Send. Provider credentials and
// working-directory changes are not hot-swapped — construct a new Session
// for those.
func (s *Session) UpdateConfig(cfg *config.Config) error {
	if s.inFlight.Load() {
		return ErrBusy
	}
	s.mu.Lock()
	merged := config.Merge(s.cfg, cfg)
	s.cfg = merged
	s.mu.Unlock()
	// Propagate agent tunables that live outside the config struct.
	if merged.Agent.MaxRetries != 0 {
		s.machine.SetMaxRetries(merged.Agent.MaxRetries)
	}
	return nil
}

// ReloadPlugins rescans the plugins directory and hot-swaps the tool set
// without restarting the session. Safe to call at any time a turn is not in
// flight. Returns the number of plugin tools loaded (native tools are always
// included but are not counted in the return value).
func (s *Session) ReloadPlugins() (int, error) {
	if s.inFlight.Load() {
		return 0, ErrBusy
	}

	allTools := tools.NewNativeTools(s.logger)
	var pluginCount int

	if s.pluginsDir != "" {
		pluginTools, err := tools.LoadPlugins(s.pluginsDir, s.logger)
		if err != nil {
			return 0, fmt.Errorf("reload plugins: %w", err)
		}
		pluginCount = len(pluginTools)
		allTools = append(allTools, pluginTools...)
	}

	// Re-include extra tools registered at construction time.
	if len(s.extraTools) > 0 {
		allTools = append(allTools, s.extraTools...)
	}

	s.mu.Lock()
	s.ctxMgr.SetTools(allTools)
	s.dispatcher = security.NewDispatcher(s.security.WrapTools(allTools)...)
	s.ungatedDispatcher = security.NewDispatcher(allTools...)
	s.modelTools = makeModelTools(allTools)
	s.mu.Unlock()

	s.logger.Info("plugins reloaded", "plugin_count", pluginCount, "total_tools", len(allTools))
	return pluginCount, nil
}

// SetPermissionCallback registers a function that is called when the security
// gate denies a tool call. The callback receives the context of the current
// turn (so it can be cancelled), the tool name, and the required vs. allowed
// permission levels. If it returns true the tool is executed once without gate
// enforcement; if false the denial propagates to the model as a tool error.
//
// The callback runs in the ReAct goroutine and may block (e.g. to await a
// user prompt). Pass nil to remove a previously registered callback.
func (s *Session) SetPermissionCallback(fn func(ctx context.Context, toolName string, required, allowed security.PermissionLevel) bool) {
	s.mu.Lock()
	s.permissionAsk = fn
	s.mu.Unlock()
}

// SetBypassMode activates unsafe bypass mode, auto-approving all gate denials
// without prompting the user. Pass a non-zero until to expire automatically;
// pass time.Time{} (zero) to keep it active for the whole session.
// Calling SetBypassMode while bypass is already active replaces the expiry.
func (s *Session) SetBypassMode(until time.Time) {
	s.mu.Lock()
	if until.IsZero() {
		s.bypassSession = true
		s.bypassUntil = time.Time{}
	} else {
		s.bypassSession = false
		s.bypassUntil = until
	}
	s.mu.Unlock()
}

// ClearBypassMode deactivates bypass mode immediately, restoring normal
// permission prompting. Safe to call even when bypass is not active.
func (s *Session) ClearBypassMode() {
	s.mu.Lock()
	s.bypassSession = false
	s.bypassUntil = time.Time{}
	s.mu.Unlock()
}

// IsBypassActive reports whether bypass mode is currently active.
func (s *Session) IsBypassActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bypassSession || (!s.bypassUntil.IsZero() && time.Now().Before(s.bypassUntil))
}

// ── Internal wiring ───────────────────────────────────────────────────────────

func (s *Session) runReActLoop(ctx context.Context) error {
	s.machine.Reset()

	// Shared turn state threaded through the phase handlers via closures.
	var (
		systemPrompt  string
		selectedProv  provider.Provider
		selectedModel model.Model
		lastMsg       model.Message
	)

	s.machine.SetHandlers(
		// gatherFn: assemble context and estimate tokens.
		func(ctx context.Context) error {
			if err := s.ctxMgr.LoadSkills(); err != nil {
				s.logger.Warn("failed to load skills", "error", err)
			}

			s.mu.RLock()
			maxBudget := s.cfg.Context.MaxBudget
			s.mu.RUnlock()
			if maxBudget == 0 {
				maxBudget = 32000
			}

			prompt, err := s.ctxMgr.AssembleContext(ctx, maxBudget)
			if err != nil {
				s.logger.Error("gather: context assembly failed", "error", err)
				return fmt.Errorf("gather: context assembly failed: %w", err)
			}
			systemPrompt = prompt
			s.logger.Debug("gather: context assembled", "prompt_bytes", len(prompt), "budget", maxBudget)
			return nil
		},

		// actFn: run model inference.
		func(ctx context.Context) error {
			s.mu.RLock()
			history := make([]model.Message, len(s.history))
			copy(history, s.history)
			s.mu.RUnlock()

			// Prepend system prompt when available.
			if systemPrompt != "" {
				sysMsg := newTextMessage(model.MessageRoleSystem, systemPrompt)
				history = append([]model.Message{sysMsg}, history...)
			}

			recs, err := s.router.SelectOptimalModel(ctx, history)
			if err != nil {
				s.logger.Error("act: model selection failed", "error", err)
				return fmt.Errorf("act: model selection failed: %w", err)
			}
			// Respect the user's configured default model over TACOS ranking.
			recs = s.preferConfiguredModels(recs)
			selectedProv = recs[0].Provider
			selectedModel = recs[0].Model
			selectedProv.SetModel(selectedModel)

			// Only pass tools when the model declares it supports them.
			// Strip tool messages from history too — models that reject tools
			// also reject histories containing tool call / result turns.
			inferHistory := history
			var inferTools []model.Tool
			if selectedModel.SupportsTools() {
				inferTools = s.modelTools
			} else {
				if len(s.modelTools) > 0 {
					s.logger.Warn("act: model does not support tools, omitting tools and stripping tool history",
						"provider", selectedProv.GetID(),
						"model", selectedModel.GetID(),
					)
				}
				inferHistory = stripToolMessages(history)
			}

			s.logger.Info("act: starting inference",
				"provider", selectedProv.GetID(),
				"model", selectedModel.GetID(),
				"history_messages", len(inferHistory),
				"tools", len(inferTools),
			)

			msg, usage, err := selectedModel.Infer(ctx, inferHistory, model.InferOptions{
				Tools: inferTools,
			}, func(part model.MessagePart) {
				s.emit(Event{Kind: EventPartReceived, Payload: part})
			})
			if err != nil {
				s.logger.Error("act: inference failed",
					"provider", selectedProv.GetID(),
					"model", selectedModel.GetID(),
					"error", err,
				)
				return fmt.Errorf("act: inference failed: %w", err)
			}

			s.logger.Info("act: inference complete",
				"provider", selectedProv.GetID(),
				"model", selectedModel.GetID(),
				"prompt_tokens", usage.PromptTokens,
				"completion_tokens", usage.CompletionTokens,
				"duration", usage.Duration,
			)

			lastMsg = msg
			s.router.RecordUsage(selectedProv, selectedModel, usage)
			s.usage.RecordActual(usage)
			return nil
		},

		// verifyFn: dispatch tool calls or complete.
		func(ctx context.Context) error {
			if lastMsg == nil {
				return nil
			}

			var toolCalls []model.ToolCall
			for part := range lastMsg.GetParts().Iterator() {
				if tc, ok := part.GetContent().(model.ToolCall); ok {
					toolCalls = append(toolCalls, tc)
				}
			}

			if len(toolCalls) == 0 {
				// No tool calls — record the final message and signal completion.
				s.mu.Lock()
				s.history = append(s.history, lastMsg)
				s.mu.Unlock()
				s.emit(Event{Kind: EventComplete, Payload: lastMsg})
				return nil
			}

			// Abort before dispatching if the caller already cancelled.
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("verify: context cancelled before tool dispatch: %w", err)
			}

			// Append assistant tool-call message before dispatching.
			s.mu.Lock()
			s.history = append(s.history, lastMsg)
			s.mu.Unlock()

			s.logger.Info("verify: dispatching tool calls", "count", len(toolCalls))

			// Execute each tool call and accumulate results.
			resultParts := structs.NewLinkedList[model.MessagePart]()
			for _, tc := range toolCalls {
				var params map[string]any
				if tc.Arguments != "" {
					if err := json.Unmarshal([]byte(tc.Arguments), &params); err != nil {
						s.logger.Warn("verify: malformed tool arguments, calling with empty params",
							"tool", tc.Name,
							"call_id", tc.ID,
							"error", err,
						)
					}
				}
				if params == nil {
					params = map[string]any{}
				}

				result := s.dispatcher.Dispatch(tc.Name, params)

				// When the gate denies the call, either auto-approve (bypass
				// mode) or ask the user (if a callback is registered).
				if dispatchErr := result.GetError(); dispatchErr != nil {
					var permErr *security.ErrPermissionDenied
					if errors.As(dispatchErr, &permErr) {
						if s.IsBypassActive() {
							s.logger.Info("verify: bypass mode active — auto-approving",
								"tool", tc.Name,
								"required", permErr.Required,
							)
							result = s.ungatedDispatcher.Dispatch(tc.Name, params)
						} else {
							s.mu.RLock()
							askFn := s.permissionAsk
							ungated := s.ungatedDispatcher
							s.mu.RUnlock()
							if askFn != nil && askFn(ctx, permErr.ToolName, permErr.Required, permErr.Allowed) {
								s.logger.Info("verify: user approved permission elevation",
									"tool", tc.Name,
									"required", permErr.Required,
								)
								result = ungated.Dispatch(tc.Name, params)
							}
						}
					}
				}

				content := ""
				isErr := false
				if err := result.GetError(); err != nil {
					content = err.Error()
					isErr = true
					s.logger.Warn("verify: tool call error",
						"tool", tc.Name,
						"call_id", tc.ID,
						"error", err,
					)
				} else if c, ok := result.GetContent().(string); ok {
					content = c
					s.logger.Debug("verify: tool call ok",
						"tool", tc.Name,
						"call_id", tc.ID,
						"response_bytes", len(content),
					)
				}

				resultParts.PushBack(model.NewToolResultPart(model.ToolResult{
					CallID:  tc.ID,
					Name:    tc.Name,
					Content: content,
					IsError: isErr,
				}))
			}

			toolResultMsg := model.NewMessage(
				model.WithRole(model.MessageRoleTool),
				model.WithContent(resultParts),
			)
			s.mu.Lock()
			s.history = append(s.history, toolResultMsg)
			s.mu.Unlock()

			// Reset the retry counter so each tool-dispatch round does not count
			// against MaxRetries, which is reserved for genuine verify failures.
			s.machine.ResetRetries()

			// Return a non-nil error to trigger another Act phase.
			return fmt.Errorf("verify: %d tool call(s) dispatched, re-invoking model", len(toolCalls))
		},
	)

	return s.machine.Run(ctx)
}

func (s *Session) emit(e Event) {
	s.handlersMu.RLock()
	hs := make([]EventHandler, len(s.handlers))
	copy(hs, s.handlers)
	s.handlersMu.RUnlock()
	for _, h := range hs {
		h(e)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// parsePermissionLevel converts the string from config to the security package
// constant. Defaults to PermissionRead for unknown values.
func parsePermissionLevel(s string) security.PermissionLevel {
	switch s {
	case "write":
		return security.PermissionWrite
	case "bash":
		return security.PermissionBash
	case "danger_zone":
		return security.PermissionDangerZone
	default:
		return security.PermissionRead
	}
}

// buildProviders attempts to construct each configured provider. Those without
// credentials are silently skipped. Returns an error only when zero providers
// could be initialised.
func buildProviders(ctx context.Context, cfg *config.ProvidersConfig, logger *slog.Logger) ([]provider.Provider, error) {
	var provs []provider.Provider

	// Anthropic — accepts the API key directly.
	if cfg.Anthropic.APIKey != "" || os.Getenv("ANTHROPIC_API_KEY") != "" {
		if p, err := anthropicprov.NewProvider(ctx, cfg.Anthropic.APIKey, logger); err == nil {
			provs = append(provs, p)
		} else {
			logger.Warn("anthropic provider unavailable", "error", err)
		}
	}

	// OpenAI — pass the key from config directly; the provider falls back to
	// OPENAI_API_KEY when the config key is empty.
	if cfg.OpenAI.APIKey != "" || os.Getenv("OPENAI_API_KEY") != "" {
		p, err := openaiprov.NewProvider(ctx, cfg.OpenAI.APIKey, logger)
		if err == nil {
			provs = append(provs, p)
		} else {
			logger.Warn("openai provider unavailable", "error", err)
		}
	}

	// Gemini — supports API key or Vertex AI authentication.
	if cfg.Gemini.Vertex != nil && *cfg.Gemini.Vertex {
		p, err := geminiprov.NewProvider(ctx, geminiprov.Config{
			AuthType:  geminiprov.AuthTypeOAuth2,
			Vertex:    true,
			ProjectID: cfg.Gemini.ProjectID,
			Location:  cfg.Gemini.Location,
		}, logger)
		if err == nil {
			provs = append(provs, p)
		} else {
			logger.Warn("gemini vertex provider unavailable", "error", err)
		}
	} else {
		geminiKey := cfg.Gemini.APIKey
		if geminiKey == "" {
			geminiKey = os.Getenv("GEMINI_API_KEY")
		}
		if geminiKey != "" {
			p, err := geminiprov.NewProvider(ctx, geminiprov.Config{
				AuthType: geminiprov.AuthTypeAPIKey,
				APIKey:   geminiKey,
			}, logger)
			if err == nil {
				provs = append(provs, p)
			} else {
				logger.Warn("gemini provider unavailable", "error", err)
			}
		}
	}

	// Generic OpenAI-compatible endpoint — only built when BaseURL is set.
	if cfg.OpenAICompat.BaseURL != "" {
		p, err := openaicompatprov.NewProvider(ctx, openaicompatprov.Config{
			BaseURL:      cfg.OpenAICompat.BaseURL,
			APIKey:       cfg.OpenAICompat.APIKey,
			Name:         cfg.OpenAICompat.Name,
			DisableTools: cfg.OpenAICompat.DisableTools != nil && *cfg.OpenAICompat.DisableTools,
		}, logger)
		if err == nil {
			provs = append(provs, p)
		} else {
			logger.Warn("openai_compat provider unavailable", "error", err)
		}
	}

	// Ollama is deliberately omitted from the default buildProviders path
	// because its NewProvider tries to start the daemon as a side effect.
	// Embed it by passing WithProviders(ollamaProvider) to New() instead.

	if len(provs) == 0 {
		return nil, errors.New("no provider credentials found; set at least one of ANTHROPIC_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY, or OPENAI_COMPAT_BASE_URL")
	}
	return provs, nil
}

// newTextMessage builds a model.Message with a single text part.
func newTextMessage(role model.MessageRole, text string) model.Message {
	content := structs.NewLinkedList[model.MessagePart]()
	content.PushBack(model.NewTextMessagePart(role, text))
	return model.NewMessage(model.WithRole(role), model.WithContent(content))
}

// modelToolAdapter wraps tools.Tool to satisfy model.Tool without a circular import.
type modelToolAdapter struct{ inner tools.Tool }

func (a modelToolAdapter) GetName() string               { return a.inner.GetName() }
func (a modelToolAdapter) GetDescription() string        { return a.inner.GetDescription() }
func (a modelToolAdapter) GetParameters() map[string]any { return a.inner.GetParameters() }
func (a modelToolAdapter) GetLogger() *slog.Logger       { return a.inner.GetLogger() }

// makeModelTools converts tools.Tool instances into model.Tool for InferOptions.
func makeModelTools(ts []tools.Tool) []model.Tool {
	out := make([]model.Tool, len(ts))
	for i, t := range ts {
		out[i] = modelToolAdapter{inner: t}
	}
	return out
}

// preferConfiguredModels reorders recommendations so that the model explicitly
// configured as the default for its provider is tried first. TACOS may rank a
// different model higher based on latency metrics, but the user's explicit
// preference always wins when present.
func (s *Session) preferConfiguredModels(recs []agent.RouteRecommendation) []agent.RouteRecommendation {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	defaults := map[string]string{
		anthropicprov.ProviderID:    cfg.Providers.Anthropic.DefaultModel,
		openaiprov.ProviderID:       cfg.Providers.OpenAI.DefaultModel,
		geminiprov.ProviderID:       cfg.Providers.Gemini.DefaultModel,
		ollamaprov.ProviderID:       cfg.Providers.Ollama.DefaultModel,
		openaicompatprov.ProviderID: cfg.Providers.OpenAICompat.DefaultModel,
	}

	for i, rec := range recs {
		preferred := defaults[rec.Provider.GetID()]
		if preferred == "" {
			continue
		}
		if strings.EqualFold(rec.Model.GetID(), preferred) || strings.EqualFold(rec.Model.GetName(), preferred) {
			// Swap the match to position 0 so it is selected first.
			recs[0], recs[i] = recs[i], recs[0]
			break
		}
	}
	return recs
}

// stripToolMessages returns a copy of history with tool-call and tool-result
// messages removed. Used when the selected model does not support tools so
// those messages do not trigger a provider-side "tools not supported" error.
func stripToolMessages(history []model.Message) []model.Message {
	out := make([]model.Message, 0, len(history))
	for _, msg := range history {
		switch msg.GetRole() {
		case model.MessageRoleTool:
			// Drop tool-result messages entirely.
			continue
		case model.MessageRoleAssistant:
			// Drop assistant messages that contain only tool calls (no text).
			hasText := false
			for part := range msg.GetParts().Iterator() {
				if _, ok := part.GetContent().(string); ok {
					hasText = true
					break
				}
			}
			if !hasText {
				continue
			}
		}
		out = append(out, msg)
	}
	return out
}
