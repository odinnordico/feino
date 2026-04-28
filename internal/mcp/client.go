// Package mcp provides an MCP client that connects to Model Context Protocol
// servers either by spawning a local subprocess over stdio or by reaching a
// remote endpoint over streamable HTTP with server-sent events.
//
// The two entry points are [Client.ConnectStdio] and [Client.ConnectSSE].
// Both return a [Session] that exposes typed operations (ListTools, CallTool,
// ListResources, ReadResource, ListPrompts, GetPrompt). See [Session.AsTools]
// to bridge discovered MCP tools into the feino tool pipeline.
package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ClientOption configures a [Client].
type ClientOption func(*Client)

// WithClientLogger sets the slog.Logger used to record client activity.
func WithClientLogger(l *slog.Logger) ClientOption {
	return func(c *Client) { c.logger = l }
}

// WithToolListChangedHandler registers a callback that is invoked when the
// server notifies the client that its tool list has changed.
func WithToolListChangedHandler(h func(context.Context, *sdkmcp.ToolListChangedRequest)) ClientOption {
	return func(c *Client) { c.mcpOpts.ToolListChangedHandler = h }
}

// WithResourceListChangedHandler registers a callback for resource-list-changed
// notifications.
func WithResourceListChangedHandler(h func(context.Context, *sdkmcp.ResourceListChangedRequest)) ClientOption {
	return func(c *Client) { c.mcpOpts.ResourceListChangedHandler = h }
}

// WithPromptListChangedHandler registers a callback for prompt-list-changed
// notifications.
func WithPromptListChangedHandler(h func(context.Context, *sdkmcp.PromptListChangedRequest)) ClientOption {
	return func(c *Client) { c.mcpOpts.PromptListChangedHandler = h }
}

// Client is a configured MCP client capable of opening sessions to MCP servers.
// A single Client instance can be reused across multiple [Session] connections.
type Client struct {
	logger    *slog.Logger
	impl      *sdkmcp.Implementation
	mcpOpts   sdkmcp.ClientOptions
	mcpClient *sdkmcp.Client // created lazily once all options are applied
}

// NewClient creates a Client that identifies itself to servers as name/version.
func NewClient(name, version string, opts ...ClientOption) *Client {
	c := &Client{
		logger: slog.Default(),
		impl:   &sdkmcp.Implementation{Name: name, Version: version},
	}
	for _, opt := range opts {
		opt(c)
	}
	c.mcpOpts.Logger = c.logger
	c.mcpClient = sdkmcp.NewClient(c.impl, &c.mcpOpts)
	return c
}

// Connect opens a session over the provided SDK transport. This is the
// underlying primitive; prefer [ConnectStdio] or [ConnectSSE] for the two
// standard transports. It is also used in tests via [sdkmcp.NewInMemoryTransports].
func (c *Client) Connect(ctx context.Context, t sdkmcp.Transport) (*Session, error) {
	cs, err := c.mcpClient.Connect(ctx, t, nil)
	if err != nil {
		c.logger.Error("mcp: connect failed", "error", err)
		return nil, fmt.Errorf("mcp connect: %w", err)
	}
	c.logger.Info("mcp: session established")
	return &Session{cs: cs, logger: c.logger}, nil
}

// StdioClientConfig describes how to spawn and connect to a local MCP server
// subprocess over its stdin/stdout pipes.
type StdioClientConfig struct {
	// Command is the path to the MCP server binary.
	Command string

	// Args are the command-line arguments passed to the server.
	Args []string

	// Env holds additional environment variables in "KEY=VALUE" form that are
	// appended to the current process environment before the subprocess starts.
	// The subprocess always inherits the parent environment first.
	Env []string

	// TerminateDuration is how long ConnectStdio waits for the subprocess to
	// exit cleanly after closing stdin before sending SIGTERM. Zero uses the
	// SDK default of 5 seconds.
	TerminateDuration time.Duration
}

// ConnectStdio spawns the server binary described by cfg and establishes a
// session over the subprocess's stdin/stdout pipes using newline-delimited JSON.
//
// The MCP shutdown sequence is followed on [Session.Close]: stdin is closed
// first, then SIGTERM is sent if the process has not exited within
// TerminateDuration, and finally SIGKILL if it still has not exited.
func (c *Client) ConnectStdio(ctx context.Context, cfg StdioClientConfig) (*Session, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("mcp stdio: Command must not be empty")
	}

	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...) //nolint:gosec // MCP server command and args are user-configured; this is intentional by design
	cmd.Env = append(os.Environ(), cfg.Env...)

	t := &sdkmcp.CommandTransport{
		Command:           cmd,
		TerminateDuration: cfg.TerminateDuration,
	}

	c.logger.Debug("mcp: spawning stdio server", "command", cfg.Command, "args", cfg.Args)
	return c.Connect(ctx, t)
}

// SSEClientConfig describes how to connect to a remote MCP server over
// streamable HTTP with server-sent events.
type SSEClientConfig struct {
	// Endpoint is the full URL of the MCP server (e.g. "https://example.com/mcp").
	Endpoint string

	// HTTPClient is an optional custom HTTP client. When nil the default
	// http.Client is used.
	HTTPClient *http.Client

	// MaxRetries is the number of SSE reconnection attempts before giving up.
	// Zero uses the SDK default of 5. Negative values disable retries.
	MaxRetries int

	// DisableStandaloneSSE, when true, suppresses the persistent GET stream that
	// the client opens after initialization for server-initiated notifications.
	// Set this when the server does not support GET-based SSE or when you only
	// need request-response communication.
	DisableStandaloneSSE bool
}

// ConnectSSE connects to the remote MCP server at cfg.Endpoint using the
// streamable HTTP transport. After a successful initialize handshake the
// client opens a persistent SSE GET stream (unless DisableStandaloneSSE is
// set) so that the server can push notifications asynchronously.
func (c *Client) ConnectSSE(ctx context.Context, cfg SSEClientConfig) (*Session, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("mcp sse: Endpoint must not be empty")
	}

	t := &sdkmcp.StreamableClientTransport{
		Endpoint:             cfg.Endpoint,
		HTTPClient:           cfg.HTTPClient,
		MaxRetries:           cfg.MaxRetries,
		DisableStandaloneSSE: cfg.DisableStandaloneSSE,
	}

	c.logger.Debug("mcp: connecting to SSE server", "endpoint", cfg.Endpoint)
	return c.Connect(ctx, t)
}

// Session is an active, initialized connection to an MCP server. Obtain one
// via [Client.Connect], [Client.ConnectStdio], or [Client.ConnectSSE].
//
// All methods are safe for concurrent use. Call [Session.Close] when done to
// release the underlying transport connection.
type Session struct {
	cs     *sdkmcp.ClientSession
	logger *slog.Logger
}

// Close terminates the session. For stdio sessions this triggers the MCP
// shutdown sequence (close stdin → SIGTERM → SIGKILL).
func (s *Session) Close() error {
	s.logger.Debug("mcp: closing session")
	return s.cs.Close()
}

// ServerInfo returns the implementation metadata advertised by the server
// during initialization, or nil if the session has not yet been initialized.
func (s *Session) ServerInfo() *sdkmcp.Implementation {
	res := s.cs.InitializeResult()
	if res == nil {
		return nil
	}
	return res.ServerInfo
}

// ListTools returns all tools advertised by the server, transparently
// exhausting any cursor-based pagination.
func (s *Session) ListTools(ctx context.Context) ([]*sdkmcp.Tool, error) {
	var out []*sdkmcp.Tool
	for t, err := range s.cs.Tools(ctx, nil) {
		if err != nil {
			s.logger.Error("mcp: list tools failed", "error", err)
			return nil, fmt.Errorf("mcp list tools: %w", err)
		}
		out = append(out, t)
	}
	s.logger.Debug("mcp: listed tools", "count", len(out))
	return out, nil
}

// CallTool invokes the named tool on the server with the given arguments.
// args must be JSON-serializable; nil is acceptable when the tool takes no
// parameters.
//
// CallTool returns a non-nil error in two cases: (1) a transport- or
// protocol-level failure where the SDK itself reports an error (the result
// will be nil); (2) the server returns a result with IsError=true (e.g.
// schema validation failure or an exception in the tool implementation) — in
// that case the result is also returned so callers that want the original
// content blocks can still inspect them.
func (s *Session) CallTool(ctx context.Context, name string, args map[string]any) (*sdkmcp.CallToolResult, error) {
	s.logger.Debug("mcp: calling tool", "tool", name)
	params := &sdkmcp.CallToolParams{Name: name, Arguments: args}
	result, err := s.cs.CallTool(ctx, params)
	if err != nil {
		s.logger.Error("mcp: call tool failed", "tool", name, "error", err)
		return nil, fmt.Errorf("mcp call tool %q: %w", name, err)
	}
	if result.IsError {
		msg := errorMessageFromResult(result)
		s.logger.Warn("mcp: tool returned an error result", "tool", name, "error", msg)
		return result, fmt.Errorf("mcp call tool %q: %s", name, msg)
	}
	s.logger.Debug("mcp: tool call succeeded", "tool", name)
	return result, nil
}

// errorMessageFromResult builds a human-readable message from a
// CallToolResult whose IsError flag is true. It joins the text of every
// TextContent block in the result; if there are none it returns a generic
// fallback so the surfaced error is never empty.
func errorMessageFromResult(result *sdkmcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range result.Content {
		tc, ok := c.(*sdkmcp.TextContent)
		if !ok {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("; ")
		}
		sb.WriteString(tc.Text)
	}
	if sb.Len() == 0 {
		return "tool reported an error with no text content"
	}
	return sb.String()
}

// ListResources returns all resources advertised by the server, transparently
// exhausting cursor-based pagination.
func (s *Session) ListResources(ctx context.Context) ([]*sdkmcp.Resource, error) {
	var out []*sdkmcp.Resource
	for r, err := range s.cs.Resources(ctx, nil) {
		if err != nil {
			s.logger.Error("mcp: list resources failed", "error", err)
			return nil, fmt.Errorf("mcp list resources: %w", err)
		}
		out = append(out, r)
	}
	s.logger.Debug("mcp: listed resources", "count", len(out))
	return out, nil
}

// ReadResource fetches the contents of the resource identified by uri.
func (s *Session) ReadResource(ctx context.Context, uri string) (*sdkmcp.ReadResourceResult, error) {
	result, err := s.cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	if err != nil {
		return nil, fmt.Errorf("mcp read resource %q: %w", uri, err)
	}
	return result, nil
}

// ListPrompts returns all prompts advertised by the server, transparently
// exhausting cursor-based pagination.
func (s *Session) ListPrompts(ctx context.Context) ([]*sdkmcp.Prompt, error) {
	var out []*sdkmcp.Prompt
	for p, err := range s.cs.Prompts(ctx, nil) {
		if err != nil {
			s.logger.Error("mcp: list prompts failed", "error", err)
			return nil, fmt.Errorf("mcp list prompts: %w", err)
		}
		out = append(out, p)
	}
	s.logger.Debug("mcp: listed prompts", "count", len(out))
	return out, nil
}

// GetPrompt retrieves a named prompt template from the server, substituting
// args into any template parameters. args may be nil.
func (s *Session) GetPrompt(ctx context.Context, name string, args map[string]string) (*sdkmcp.GetPromptResult, error) {
	params := &sdkmcp.GetPromptParams{Name: name, Arguments: args}
	result, err := s.cs.GetPrompt(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("mcp get prompt %q: %w", name, err)
	}
	return result, nil
}
