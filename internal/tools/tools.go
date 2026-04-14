package tools

import (
	"log/slog"
)

// PermLevel* constants define the permission level a tool requires to execute.
// These values correspond 1-to-1 with security.PermissionLevel and are defined
// here as plain ints to avoid an import cycle with internal/security.
const (
	PermLevelRead       = 0
	PermLevelWrite      = 1
	PermLevelBash       = 2
	PermLevelDangerZone = 3
	permLevelUnset      = -1 // sentinel: tool has not self-declared a level
)

// Classified is an optional interface a Tool may implement to self-declare the
// minimum permission level required to run it. The security gateway uses this
// value in preference to any external classification map.
// Return a negative value to indicate no level is declared.
type Classified interface {
	PermissionLevel() int
}

type ToolResult interface {
	GetContent() any
	GetError() error
}

type toolResultImpl struct {
	content any
	err     error
}

func (r *toolResultImpl) GetContent() any { return r.content }
func (r *toolResultImpl) GetError() error { return r.err }

// NewToolResult constructs a ToolResult with the given content and optional error.
func NewToolResult(content any, err error) ToolResult {
	return &toolResultImpl{content: content, err: err}
}

type Tool interface {
	GetName() string
	GetDescription() string
	GetParameters() map[string]any
	Run(parameters map[string]any) ToolResult
	GetLogger() *slog.Logger
}

// ToolOption configures a toolImpl at construction time.
type ToolOption func(*toolImpl)

// WithPermissionLevel sets the tool's self-declared permission level.
// Use the PermLevel* constants defined in this package.
func WithPermissionLevel(level int) ToolOption {
	return func(t *toolImpl) { t.level = level }
}

// WithLogger sets the tool's logger.
func WithLogger(l *slog.Logger) ToolOption {
	return func(t *toolImpl) { t.logger = l }
}

type toolImpl struct {
	name        string
	description string
	parameters  map[string]any
	run         func(parameters map[string]any) ToolResult
	level       int // permLevelUnset (-1) when not declared
	logger      *slog.Logger
}

func (t *toolImpl) GetName() string                          { return t.name }
func (t *toolImpl) GetDescription() string                   { return t.description }
func (t *toolImpl) GetParameters() map[string]any            { return t.parameters }
func (t *toolImpl) Run(parameters map[string]any) ToolResult { return t.run(parameters) }
func (t *toolImpl) PermissionLevel() int                     { return t.level }
func (t *toolImpl) GetLogger() *slog.Logger {
	if t.logger == nil {
		return slog.Default()
	}
	return t.logger
}

// NewTool constructs a Tool. parameters must be a JSON Schema map[string]any
// describing the tool's input (used by providers to generate function-calling schemas).
// Use WithPermissionLevel to self-declare the required security level.
func NewTool(name, description string, parameters map[string]any, run func(map[string]any) ToolResult, opts ...ToolOption) Tool {
	impl := &toolImpl{
		name:        name,
		description: description,
		parameters:  parameters,
		run:         run,
		level:       permLevelUnset,
		logger:      slog.Default(),
	}
	for _, opt := range opts {
		opt(impl)
	}
	return impl
}
