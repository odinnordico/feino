package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/odinnordico/feino/internal/tools"
)

// mcpToolAdapter adapts a single MCP tool and the session it lives on into the
// feino tools.Tool interface. This lets MCP-discovered tools flow into the
// agent pipeline without any changes to the existing tool machinery.
//
// Run uses context.Background() because tools.Tool.Run does not accept a
// context. Cancel an MCP tool call by closing the parent Session instead.
type mcpToolAdapter struct {
	tool    *sdkmcp.Tool
	session *Session
}

func (a *mcpToolAdapter) GetLogger() *slog.Logger {
	return a.session.logger
}

// GetName returns the tool's MCP name.
func (a *mcpToolAdapter) GetName() string { return a.tool.Name }

// GetDescription returns the tool's human-readable description.
func (a *mcpToolAdapter) GetDescription() string { return a.tool.Description }

// GetParameters returns the tool's JSON Schema input definition as a
// map[string]any. On the client side the SDK always deserializes InputSchema
// to map[string]any; if it cannot be cast (e.g. the tool has no schema) nil
// is returned.
func (a *mcpToolAdapter) GetParameters() map[string]any {
	if a.tool.InputSchema == nil {
		return nil
	}
	m, ok := a.tool.InputSchema.(map[string]any)
	if !ok {
		return nil
	}
	return m
}

// Run calls the MCP tool synchronously and converts the response into a
// tools.ToolResult. Text content blocks from the server are concatenated into
// a single string; other content types (images, resources) are silently
// skipped. When the server sets IsError on the result, the returned
// ToolResult.GetError() will be non-nil.
func (a *mcpToolAdapter) Run(params map[string]any) tools.ToolResult {
	a.session.logger.Debug("mcp: running tool via bridge", "tool", a.tool.Name)
	result, err := a.session.CallTool(context.Background(), a.tool.Name, params)
	if err != nil {
		a.session.logger.Error("mcp: bridge tool call failed", "tool", a.tool.Name, "error", err)
		return &adapterResult{err: err}
	}

	var sb strings.Builder
	for _, c := range result.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}

	text := sb.String()
	var callErr error
	if result.IsError {
		callErr = fmt.Errorf("mcp tool %q: %s", a.tool.Name, text)
	}
	return &adapterResult{content: text, err: callErr}
}

// adapterResult is a minimal tools.ToolResult backed by a string content
// value and an optional error.
type adapterResult struct {
	content string
	err     error
}

func (r *adapterResult) GetContent() any { return r.content }
func (r *adapterResult) GetError() error { return r.err }

// AsTool returns a tools.Tool adapter for the single named MCP tool. It
// queries the server to confirm the tool exists and to obtain its current
// schema.
func (s *Session) AsTool(ctx context.Context, name string) (tools.Tool, error) {
	all, err := s.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("mcp: listing tools to find %q: %w", name, err)
	}
	for _, t := range all {
		if t.Name == name {
			return &mcpToolAdapter{tool: t, session: s}, nil
		}
	}
	return nil, fmt.Errorf("mcp tool %q not found on server", name)
}

// AsTools converts every tool currently advertised by the server into a
// tools.Tool and returns them as a slice. The slice is safe to pass directly
// to [agent.SubordinateTask.InferFn] tool lists or to
// [context.FileSystemContextManager.SetTools].
func (s *Session) AsTools(ctx context.Context) ([]tools.Tool, error) {
	all, err := s.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("mcp: listing tools for bridge: %w", err)
	}
	out := make([]tools.Tool, len(all))
	for i, t := range all {
		out[i] = &mcpToolAdapter{tool: t, session: s}
	}
	return out, nil
}
