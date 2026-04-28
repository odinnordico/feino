package security

import (
	"fmt"
	"log/slog"
	"maps"

	"github.com/odinnordico/feino/internal/tools"
)

// GateOption configures a SecurityGate.
type GateOption func(*Gate)

// WithGateLogger sets the logger used to record gate decisions.
func WithGateLogger(l *slog.Logger) GateOption {
	return func(g *Gate) { g.logger = l }
}

// WithDenyCallback registers a function called synchronously whenever a tool
// invocation is denied. The callback receives the tool name, the required level,
// and the current maximum allowed level.
func WithDenyCallback(fn func(toolName string, required, allowed PermissionLevel)) GateOption {
	return func(g *Gate) { g.onDeny = fn }
}

// WithPathPolicy attaches a PathPolicy to the gate. When set, every tool
// invocation that targets a filesystem path is checked against the policy after
// the permission-level check passes. Invocations whose path is not within an
// approved root are denied with a distinct error.
//
// Tools without a path parameter (e.g. shell_exec) are unaffected.
func WithPathPolicy(pp *PathPolicy) GateOption {
	return func(g *Gate) { g.pathPolicy = pp }
}

// WithASTBlacklist attaches an ASTBlacklist to the gate. When set, every
// shell_exec invocation has its command string parsed and walked as a bash AST.
// Any prohibited network or destructive filesystem operation found — at any
// nesting depth — causes an absolute denial regardless of the gate's permission
// level.
func WithASTBlacklist(bl *ASTBlacklist) GateOption {
	return func(g *Gate) { g.astBlacklist = bl }
}

// WithExtraToolLevels merges additional tool-to-level mappings into the gate.
// These take precedence over DefaultToolLevels and are useful for classifying
// dynamically-discovered tools (e.g. MCP tools). Entries are copied so later
// mutations to the caller's map do not affect the gate.
func WithExtraToolLevels(levels map[string]PermissionLevel) GateOption {
	return func(g *Gate) {
		maps.Copy(g.extraLevels, levels)
	}
}

// Gate enforces a maximum permission level on all tool invocations.
// Construct one with NewSecurityGate; it is safe for concurrent use after construction.
type Gate struct {
	maxLevel     PermissionLevel
	extraLevels  map[string]PermissionLevel
	logger       *slog.Logger
	onDeny       func(toolName string, required, allowed PermissionLevel)
	pathPolicy   *PathPolicy
	astBlacklist *ASTBlacklist
}

// permLevelUnset is the sentinel returned by gatedTool.PermissionLevel when
// the inner tool does not implement tools.Classified. LevelForTool treats any
// value < 0 as "not declared", falling through to PermissionDangerZone.
const permLevelUnset = -1

// ErrPermissionDenied is the typed error returned when a tool call is blocked
// by the security gate. Callers can use errors.As to inspect the levels and
// decide whether to prompt the user for an explicit approval.
type ErrPermissionDenied struct {
	ToolName string
	Required PermissionLevel
	Allowed  PermissionLevel
}

func (e *ErrPermissionDenied) Error() string {
	return fmt.Sprintf("security: tool %q requires %s permission, current mode allows up to %s",
		e.ToolName, e.Required, e.Allowed)
}

// ErrPathDenied is the typed error returned when a tool call is blocked
// because its path argument falls outside the approved roots configured via
// PathPolicy. Callers can use errors.As to retrieve the tool name and path.
type ErrPathDenied struct {
	ToolName string
	Path     string
}

func (e *ErrPathDenied) Error() string {
	return fmt.Sprintf("security: tool %q path %q is not in the approved path list",
		e.ToolName, e.Path)
}

// NewSecurityGate creates a SecurityGate that allows tool invocations up to
// maxLevel. Invocations requiring a higher level are denied.
func NewSecurityGate(maxLevel PermissionLevel, opts ...GateOption) *Gate {
	g := &Gate{
		maxLevel:    maxLevel,
		extraLevels: make(map[string]PermissionLevel),
		logger:      slog.Default(),
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Check evaluates whether invoking t with params is permitted under the gate's
// current maximum level and any configured policies. Returns nil if allowed, or
// a descriptive error if denied.
//
// Enforcement order:
//  1. Permission level — denied if the tool's required level exceeds maxLevel.
//  2. Path policy     — denied if the resolved path is not within an approved root.
//  3. AST blacklist   — denied if the command contains a prohibited network or FS operation.
func (g *Gate) Check(t tools.Tool, params map[string]any) error {
	required := LevelForTool(t, params, g.extraLevels)
	if required > g.maxLevel {
		if g.onDeny != nil {
			g.onDeny(t.GetName(), required, g.maxLevel)
		}
		g.logger.Warn("security gate denied", "tool", t.GetName(), "required", required, "max", g.maxLevel)
		return &ErrPermissionDenied{ToolName: t.GetName(), Required: required, Allowed: g.maxLevel}
	}

	if g.pathPolicy != nil {
		if pathStr, needsCheck := extractCheckPath(t.GetName(), params); needsCheck {
			if !g.pathPolicy.IsAllowed(pathStr) {
				g.logger.Warn("security gate denied path", "tool", t.GetName(), "path", pathStr)
				return &ErrPathDenied{ToolName: t.GetName(), Path: pathStr}
			}
		}
	}

	if g.astBlacklist != nil && t.GetName() == "shell_exec" {
		if command, _ := params["command"].(string); command != "" {
			violations, err := g.astBlacklist.Scan(command)
			if err != nil {
				g.logger.Warn("security gate denied: shell parse error", "tool", t.GetName())
				return fmt.Errorf("security: tool %q command could not be parsed: %w", t.GetName(), err)
			}
			if len(violations) > 0 {
				g.logger.Warn("security gate denied: AST violation",
					"tool", t.GetName(), "command", violations[0].Command, "reason", violations[0].Reason)
				return fmt.Errorf("security: tool %q command contains prohibited operation: %s (%s)",
					t.GetName(), violations[0].Command, violations[0].Reason)
			}
		}
	}

	g.logger.Debug("security gate allowed", "tool", t.GetName(), "required", required, "max", g.maxLevel)
	return nil
}

// WrapTool returns a tools.Tool whose Run method is intercepted by the gate.
// Metadata methods (GetName, GetDescription, GetParameters) pass through unchanged,
// so providers can still use the tool for schema generation.
func (g *Gate) WrapTool(t tools.Tool) tools.Tool {
	return &gatedTool{inner: t, gate: g}
}

// WrapTools wraps every tool in ts and returns a new slice. The original slice
// is not modified.
func (g *Gate) WrapTools(ts []tools.Tool) []tools.Tool {
	wrapped := make([]tools.Tool, len(ts))
	for i, t := range ts {
		wrapped[i] = g.WrapTool(t)
	}
	return wrapped
}

// gatedTool intercepts Run to enforce the gate's permission policy.
type gatedTool struct {
	inner tools.Tool
	gate  *Gate
}

func (t *gatedTool) GetName() string               { return t.inner.GetName() }
func (t *gatedTool) GetDescription() string        { return t.inner.GetDescription() }
func (t *gatedTool) GetParameters() map[string]any { return t.inner.GetParameters() }
func (t *gatedTool) GetLogger() *slog.Logger       { return t.inner.GetLogger() }

// PermissionLevel implements tools.Classified by forwarding to the inner tool,
// making gatedTool transparent with respect to permission level queries.
func (t *gatedTool) PermissionLevel() int {
	if c, ok := t.inner.(tools.Classified); ok {
		return c.PermissionLevel()
	}
	return permLevelUnset // will fall through to DangerZone in LevelForTool
}

func (t *gatedTool) Run(params map[string]any) tools.ToolResult {
	if err := t.gate.Check(t.inner, params); err != nil {
		return tools.NewToolResult("", err)
	}
	return t.inner.Run(params)
}

// Dispatcher maps tool names to tools.Tool instances and routes invocations.
// Populate it with gate-wrapped tools so every dispatch is subject to the gate.
// The dispatcher itself is policy-neutral.
//
// The registry is written only during NewDispatcher and is read-only thereafter,
// so Has and Dispatch are safe for concurrent use without additional locking.
type Dispatcher struct {
	registry map[string]tools.Tool
}

// NewDispatcher builds a Dispatcher keyed by each tool's GetName(). If two
// tools share a name, the last one wins.
func NewDispatcher(ts ...tools.Tool) *Dispatcher {
	d := &Dispatcher{registry: make(map[string]tools.Tool, len(ts))}
	for _, t := range ts {
		d.registry[t.GetName()] = t
	}
	return d
}

// Has reports whether a tool with the given name is registered.
func (d *Dispatcher) Has(name string) bool {
	_, ok := d.registry[name]
	return ok
}

// Dispatch looks up name in the registry and calls its Run method with params.
// If name is not registered, it returns a ToolResult whose GetError is non-nil.
// Gate enforcement fires inside Run when the tool is a gatedTool.
func (d *Dispatcher) Dispatch(name string, params map[string]any) tools.ToolResult {
	t, ok := d.registry[name]
	if !ok {
		return tools.NewToolResult("", fmt.Errorf("security: tool %q is not registered", name))
	}
	return t.Run(params)
}
