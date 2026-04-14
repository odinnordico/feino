package mcp_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	feino_mcp "github.com/odinnordico/feino/internal/mcp"
)

// testPair wires a pre-configured mcp.Server to a feino Client over an
// in-memory transport. No subprocess or network is required.
// The returned Session is already initialized; the caller must call
// session.Close() when done.
func testPair(t *testing.T, server *sdkmcp.Server) *feino_mcp.Session {
	t.Helper()
	ctx := context.Background()
	t1, t2 := sdkmcp.NewInMemoryTransports()

	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server.Connect: %v", err)
	}

	client := feino_mcp.NewClient("feino-test", "v0.0.0")
	session, err := client.Connect(ctx, t2)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// echoArgs is the input schema for the "echo" tool.
type echoArgs struct {
	Message string `json:"message"`
}

// newEchoServer returns a server with a single "echo" tool that reflects the
// "message" argument back as text content, a resource at "file:///hello.txt",
// and a "greet" prompt.
func newEchoServer() *sdkmcp.Server {
	server := sdkmcp.NewServer(
		&sdkmcp.Implementation{Name: "echo-server", Version: "v1.0.0"},
		nil,
	)

	// Tool: echo — use generic AddTool so InputSchema is auto-generated.
	sdkmcp.AddTool(server,
		&sdkmcp.Tool{Name: "echo", Description: "echoes its input"},
		func(_ context.Context, _ *sdkmcp.CallToolRequest, args echoArgs) (*sdkmcp.CallToolResult, any, error) {
			return &sdkmcp.CallToolResult{
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: args.Message}},
			}, nil, nil
		},
	)

	// Resource: file:///hello.txt
	server.AddResource(
		&sdkmcp.Resource{
			URI:      "file:///hello.txt",
			Name:     "hello",
			MIMEType: "text/plain",
		},
		func(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			return &sdkmcp.ReadResourceResult{
				Contents: []*sdkmcp.ResourceContents{
					{URI: req.Params.URI, MIMEType: "text/plain", Text: "hello, world"},
				},
			}, nil
		},
	)

	// Prompt: greet
	server.AddPrompt(
		&sdkmcp.Prompt{
			Name:        "greet",
			Description: "a greeting prompt",
			Arguments: []*sdkmcp.PromptArgument{
				{Name: "name", Description: "who to greet", Required: true},
			},
		},
		func(_ context.Context, req *sdkmcp.GetPromptRequest) (*sdkmcp.GetPromptResult, error) {
			name := req.Params.Arguments["name"]
			return &sdkmcp.GetPromptResult{
				Messages: []*sdkmcp.PromptMessage{
					{
						Role: "user",
						Content: &sdkmcp.TextContent{
							Text: "Hello, " + name + "!",
						},
					},
				},
			}, nil
		},
	)

	return server
}

// ── Server metadata ──────────────────────────────────────────────────────────

func TestSession_ServerInfo(t *testing.T) {
	session := testPair(t, newEchoServer())

	info := session.ServerInfo()
	if info == nil {
		t.Fatal("ServerInfo returned nil")
	}
	if info.Name != "echo-server" {
		t.Errorf("expected Name %q, got %q", "echo-server", info.Name)
	}
	if info.Version != "v1.0.0" {
		t.Errorf("expected Version %q, got %q", "v1.0.0", info.Version)
	}
}

// ── Tools ────────────────────────────────────────────────────────────────────

func TestSession_ListTools(t *testing.T) {
	session := testPair(t, newEchoServer())

	toolList, err := session.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(toolList) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(toolList))
	}
	if toolList[0].Name != "echo" {
		t.Errorf("expected tool name %q, got %q", "echo", toolList[0].Name)
	}
	if toolList[0].Description != "echoes its input" {
		t.Errorf("unexpected description: %q", toolList[0].Description)
	}
}

func TestSession_CallTool(t *testing.T) {
	session := testPair(t, newEchoServer())

	result, err := session.CallTool(context.Background(), "echo", map[string]any{
		"message": "ping",
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned IsError=true")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty content")
	}
	tc, ok := result.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("expected *TextContent, got %T", result.Content[0])
	}
	if tc.Text != "ping" {
		t.Errorf("expected %q, got %q", "ping", tc.Text)
	}
}

func TestSession_CallTool_NilArgs(t *testing.T) {
	// Calling with nil args against a tool whose schema marks fields as
	// required must surface a schema validation error — not panic.
	session := testPair(t, newEchoServer())
	_, err := session.CallTool(context.Background(), "echo", nil)
	if err == nil {
		t.Fatal("expected schema validation error for missing required args, got nil")
	}
}

func TestSession_CallTool_UnknownName(t *testing.T) {
	session := testPair(t, newEchoServer())
	_, err := session.CallTool(context.Background(), "does-not-exist", nil)
	if err == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
}

// ── Resources ────────────────────────────────────────────────────────────────

func TestSession_ListResources(t *testing.T) {
	session := testPair(t, newEchoServer())

	resources, err := session.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	if resources[0].URI != "file:///hello.txt" {
		t.Errorf("expected URI %q, got %q", "file:///hello.txt", resources[0].URI)
	}
}

func TestSession_ReadResource(t *testing.T) {
	session := testPair(t, newEchoServer())

	result, err := session.ReadResource(context.Background(), "file:///hello.txt")
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(result.Contents) == 0 {
		t.Fatal("expected non-empty contents")
	}
	if result.Contents[0].Text != "hello, world" {
		t.Errorf("expected %q, got %q", "hello, world", result.Contents[0].Text)
	}
}

func TestSession_ReadResource_UnknownURI(t *testing.T) {
	session := testPair(t, newEchoServer())
	_, err := session.ReadResource(context.Background(), "file:///no-such-file.txt")
	if err == nil {
		t.Fatal("expected error for unknown URI, got nil")
	}
}

// ── Prompts ──────────────────────────────────────────────────────────────────

func TestSession_ListPrompts(t *testing.T) {
	session := testPair(t, newEchoServer())

	prompts, err := session.ListPrompts(context.Background())
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	if len(prompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(prompts))
	}
	if prompts[0].Name != "greet" {
		t.Errorf("expected %q, got %q", "greet", prompts[0].Name)
	}
}

func TestSession_GetPrompt(t *testing.T) {
	session := testPair(t, newEchoServer())

	result, err := session.GetPrompt(context.Background(), "greet", map[string]string{
		"name": "Diego",
	})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if len(result.Messages) == 0 {
		t.Fatal("expected non-empty messages")
	}
	tc, ok := result.Messages[0].Content.(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("expected *TextContent, got %T", result.Messages[0].Content)
	}
	if tc.Text != "Hello, Diego!" {
		t.Errorf("expected %q, got %q", "Hello, Diego!", tc.Text)
	}
}

// ── Lifecycle ────────────────────────────────────────────────────────────────

func TestSession_Close_Idempotent(t *testing.T) {
	// Close may legitimately be called twice (e.g. deferred + explicit).
	// It should not panic; an error on the second call is acceptable.
	session := testPair(t, newEchoServer())

	if err := session.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// Second close: accept any outcome, just must not panic.
	_ = session.Close()
}

// ── Bridge ───────────────────────────────────────────────────────────────────

func TestSession_AsTools_ImplementsInterface(t *testing.T) {
	session := testPair(t, newEchoServer())

	adapted, err := session.AsTools(context.Background())
	if err != nil {
		t.Fatalf("AsTools: %v", err)
	}
	if len(adapted) != 1 {
		t.Fatalf("expected 1 adapted tool, got %d", len(adapted))
	}

}

func TestSession_AsTools_Metadata(t *testing.T) {
	session := testPair(t, newEchoServer())

	adapted, err := session.AsTools(context.Background())
	if err != nil {
		t.Fatalf("AsTools: %v", err)
	}
	tool := adapted[0]

	if tool.GetName() != "echo" {
		t.Errorf("expected name %q, got %q", "echo", tool.GetName())
	}
	if tool.GetDescription() != "echoes its input" {
		t.Errorf("expected description %q, got %q", "echoes its input", tool.GetDescription())
	}
}

func TestSession_AsTools_Run(t *testing.T) {
	session := testPair(t, newEchoServer())

	adapted, err := session.AsTools(context.Background())
	if err != nil {
		t.Fatalf("AsTools: %v", err)
	}

	result := adapted[0].Run(map[string]any{"message": "hello from bridge"})
	if result.GetError() != nil {
		t.Fatalf("Run returned error: %v", result.GetError())
	}
	content, ok := result.GetContent().(string)
	if !ok {
		t.Fatalf("expected string content, got %T", result.GetContent())
	}
	if content != "hello from bridge" {
		t.Errorf("expected %q, got %q", "hello from bridge", content)
	}
}

func TestSession_AsTool_Found(t *testing.T) {
	session := testPair(t, newEchoServer())

	tool, err := session.AsTool(context.Background(), "echo")
	if err != nil {
		t.Fatalf("AsTool: %v", err)
	}
	if tool.GetName() != "echo" {
		t.Errorf("expected %q, got %q", "echo", tool.GetName())
	}
}

func TestSession_AsTool_NotFound(t *testing.T) {
	session := testPair(t, newEchoServer())

	_, err := session.AsTool(context.Background(), "ghost-tool")
	if err == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
	if !strings.Contains(err.Error(), "ghost-tool") {
		t.Errorf("expected error to mention tool name, got: %v", err)
	}
}

func TestSession_AsTools_Run_IsError(t *testing.T) {
	// A server that returns IsError=true must surface an error via GetError().
	server := sdkmcp.NewServer(
		&sdkmcp.Implementation{Name: "error-server", Version: "v1.0.0"},
		nil,
	)
	sdkmcp.AddTool(server,
		&sdkmcp.Tool{Name: "fail", Description: "always fails"},
		func(_ context.Context, _ *sdkmcp.CallToolRequest, _ any) (*sdkmcp.CallToolResult, any, error) {
			res := &sdkmcp.CallToolResult{}
			res.SetError(errors.New("intentional failure"))
			return res, nil, nil
		},
	)

	session := testPair(t, server)

	adapted, err := session.AsTools(context.Background())
	if err != nil {
		t.Fatalf("AsTools: %v", err)
	}

	result := adapted[0].Run(nil)
	if result.GetError() == nil {
		t.Fatal("expected GetError() to be non-nil for IsError tool result")
	}
	if !strings.Contains(result.GetError().Error(), "intentional failure") {
		t.Errorf("expected error to contain %q, got %v", "intentional failure", result.GetError())
	}
}

// ── Config validation ─────────────────────────────────────────────────────────

func TestConnectStdio_EmptyCommand(t *testing.T) {
	c := feino_mcp.NewClient("feino", "v0.0.0")
	_, err := c.ConnectStdio(context.Background(), feino_mcp.StdioClientConfig{})
	if err == nil {
		t.Fatal("expected error for empty command, got nil")
	}
}

func TestConnectSSE_EmptyEndpoint(t *testing.T) {
	c := feino_mcp.NewClient("feino", "v0.0.0")
	_, err := c.ConnectSSE(context.Background(), feino_mcp.SSEClientConfig{})
	if err == nil {
		t.Fatal("expected error for empty endpoint, got nil")
	}
}

func TestSession_AsTools_ZeroTools(t *testing.T) {
	// A server with no registered tools must return an empty (non-nil) slice
	// and no error.
	server := sdkmcp.NewServer(
		&sdkmcp.Implementation{Name: "empty-server", Version: "v1.0.0"},
		nil,
	)
	session := testPair(t, server)

	adapted, err := session.AsTools(context.Background())
	if err != nil {
		t.Fatalf("AsTools on empty server: %v", err)
	}
	if len(adapted) != 0 {
		t.Errorf("expected 0 tools, got %d", len(adapted))
	}
}
