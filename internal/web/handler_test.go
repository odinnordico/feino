package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	feinov1 "github.com/odinnordico/feino/gen/feino/v1"
	"github.com/odinnordico/feino/gen/feino/v1/feinov1connect"
	"github.com/odinnordico/feino/internal/app"
	"github.com/odinnordico/feino/internal/config"
	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/provider"
	"github.com/odinnordico/feino/internal/structs"
)

// ── Test doubles ─────────────────────────────────────────────────────────────

type testModel struct {
	name string
	text string
	// block is closed by the test when the model should unblock.
	block chan struct{}
}

func newTestModel(name, text string) *testModel {
	return &testModel{name: name, text: text, block: make(chan struct{})}
}

func (m *testModel) GetID() string           { return m.name }
func (m *testModel) GetName() string         { return m.name }
func (m *testModel) GetDescription() string  { return "test" }
func (m *testModel) GetHomepage() string     { return "" }
func (m *testModel) GetLogger() *slog.Logger { return slog.Default() }
func (m *testModel) GetContextWindow() int   { return 128000 }
func (m *testModel) GetMaxOutputTokens() int { return 4096 }
func (m *testModel) SupportsTools() bool     { return false }

func (m *testModel) Infer(ctx context.Context, _ []model.Message, _ model.InferOptions, onPart func(model.MessagePart)) (model.Message, model.Usage, error) {
	// Wait until unblocked (or closed immediately for non-blocking tests),
	// respecting context cancellation so CancelTurn can abort inference.
	select {
	case <-ctx.Done():
		return nil, model.Usage{}, ctx.Err()
	case <-m.block:
	}

	part := model.NewTextMessagePart(model.MessageRoleAssistant, m.text)
	if onPart != nil {
		onPart(part)
	}
	content := structs.NewLinkedList[model.MessagePart]()
	content.PushBack(part)
	msg := model.NewMessage(
		model.WithRole(model.MessageRoleAssistant),
		model.WithContent(content),
	)
	return msg, model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}, nil
}

type testProvider struct {
	id  string
	mdl *testModel
	cb  *provider.CircuitBreaker
}

func newTestProvider(id, modelName, text string) *testProvider {
	return &testProvider{
		id:  id,
		mdl: newTestModel(modelName, text),
		cb:  provider.DefaultCircuitBreaker(slog.Default()),
	}
}

func (p *testProvider) GetName() string                             { return p.id }
func (p *testProvider) GetID() string                               { return p.id }
func (p *testProvider) GetDescription() string                      { return "test provider" }
func (p *testProvider) GetHomepage() string                         { return "" }
func (p *testProvider) GetLogger() *slog.Logger                     { return slog.Default() }
func (p *testProvider) GetCircuitBreaker() *provider.CircuitBreaker { return p.cb }
func (p *testProvider) GetMetrics() *provider.Metrics               { return &provider.Metrics{} }
func (p *testProvider) GetSelectedModel() model.Model               { return p.mdl }
func (p *testProvider) SetModel(_ model.Model)                      {}
func (p *testProvider) GetModels(_ context.Context) ([]model.Model, error) {
	return []model.Model{p.mdl}, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// newTestHandler creates a FeinoServiceHandler backed by a test session.
func newTestHandler(t *testing.T, prov *testProvider) *FeinoServiceHandler {
	t.Helper()
	sess, err := app.New(
		&config.Config{},
		app.WithLogger(slog.Default()),
		app.WithProviders(prov),
	)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	sm := NewSessionManager(sess)
	mhub := newMetricsHub(sess)
	fileSvc, err := newFileService("")
	if err != nil {
		t.Fatalf("newFileService: %v", err)
	}
	return &FeinoServiceHandler{
		sess:    sess,
		sm:      sm,
		mhub:    mhub,
		fileSvc: fileSvc,
		cfg:     &config.Config{},
		cfgPath: "",
	}
}

// newTestServer starts an httptest.Server with the Connect handler registered.
// Returns the server and a client connected to it.
func newTestServer(t *testing.T, h *FeinoServiceHandler) (*httptest.Server, feinov1connect.FeinoServiceClient) {
	t.Helper()
	mux := http.NewServeMux()
	path, rpcHandler := feinov1connect.NewFeinoServiceHandler(h)
	mux.Handle(path, rpcHandler)
	srv := httptest.NewUnstartedServer(h2c.NewHandler(mux, &http2.Server{}))
	srv.Start()
	t.Cleanup(srv.Close)

	client := feinov1connect.NewFeinoServiceClient(
		srv.Client(),
		srv.URL,
		connect.WithGRPC(),
	)
	return srv, client
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestGetSessionState_Idle verifies the health-check RPC returns the idle state.
func TestGetSessionState_Idle(t *testing.T) {
	prov := newTestProvider("stub", "stub-model", "hello")
	close(prov.mdl.block) // unblock immediately

	h := newTestHandler(t, prov)
	_, client := newTestServer(t, h)

	resp, err := client.GetSessionState(context.Background(), connect.NewRequest(&feinov1.GetSessionStateRequest{}))
	if err != nil {
		t.Fatalf("GetSessionState: %v", err)
	}
	if resp.Msg.GetBusy() {
		t.Error("expected Busy=false for idle session")
	}
}

// TestSendMessage_StreamsEvents verifies that SendMessage emits at least a
// PartReceivedEvent and a CompleteEvent for a simple text turn.
func TestSendMessage_StreamsEvents(t *testing.T) {
	prov := newTestProvider("stub", "stub-model", "hello from the model")
	close(prov.mdl.block) // unblock immediately — no blocking in this test

	h := newTestHandler(t, prov)
	_, client := newTestServer(t, h)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := client.SendMessage(ctx, connect.NewRequest(&feinov1.SendMessageRequest{
		Text: "hi",
	}))
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	defer stream.Close()

	var gotPart, gotComplete bool
	for stream.Receive() {
		msg := stream.Msg()
		switch msg.GetEvent().(type) {
		case *feinov1.SendMessageResponse_PartReceived:
			gotPart = true
		case *feinov1.SendMessageResponse_Complete:
			gotComplete = true
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if !gotPart {
		t.Error("expected at least one PartReceivedEvent")
	}
	if !gotComplete {
		t.Error("expected a CompleteEvent")
	}
}

// TestCancelTurn_StopsStream verifies that CancelTurn aborts a long-running
// inference and the stream closes promptly.
func TestCancelTurn_StopsStream(t *testing.T) {
	prov := newTestProvider("stub", "stub-model", "never")
	// Do NOT close prov.mdl.block — inference will block until we cancel.

	h := newTestHandler(t, prov)
	_, client := newTestServer(t, h)

	ctx, cancelCtx := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelCtx()

	stream, err := client.SendMessage(ctx, connect.NewRequest(&feinov1.SendMessageRequest{
		Text: "slow query",
	}))
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	defer stream.Close()

	// Give the session goroutine time to reach inference.
	time.Sleep(100 * time.Millisecond)

	_, cancelErr := client.CancelTurn(ctx, connect.NewRequest(&feinov1.CancelTurnRequest{}))
	if cancelErr != nil {
		t.Fatalf("CancelTurn: %v", cancelErr)
	}

	// After cancel, drain the stream — it should close without error or timeout.
	done := make(chan struct{})
	go func() {
		for stream.Receive() {
		}
		close(done)
	}()

	select {
	case <-done:
		// pass
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not close after CancelTurn")
	}
}

// TestResolvePermission_UnblocksAgent verifies the permission bridge:
// a tool-call denial emits a PermissionRequestEvent on the stream, and calling
// ResolvePermission unblocks the ReAct goroutine.
//
// This test uses the permission callback directly (no Connect round-trip for the
// ResolvePermission RPC) to keep the test deterministic without a real tool.
func TestResolvePermission_NotFound(t *testing.T) {
	prov := newTestProvider("stub", "stub-model", "hello")
	close(prov.mdl.block)

	h := newTestHandler(t, prov)
	_, client := newTestServer(t, h)

	// Resolving a request that was never registered should return CodeNotFound.
	_, err := client.ResolvePermission(context.Background(), connect.NewRequest(&feinov1.ResolvePermissionRequest{
		RequestId: "nonexistent-id",
		Approved:  true,
	}))
	if err == nil {
		t.Fatal("expected error for unknown request ID, got nil")
	}
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		t.Fatalf("expected connect.Error, got %T: %v", err, err)
	}
	if connectErr.Code() != connect.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", connectErr.Code())
	}
}

// TestSessionManager_Subscribe verifies that events sent to the session
// are forwarded to the subscribed channel.
func TestSessionManager_Subscribe(t *testing.T) {
	prov := newTestProvider("stub", "stub-model", "hello")
	close(prov.mdl.block)

	sess, err := app.New(
		&config.Config{},
		app.WithLogger(slog.Default()),
		app.WithProviders(prov),
	)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	sm := NewSessionManager(sess)
	eventCh, cancel := sm.Subscribe("test-stream")
	defer cancel()

	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()

	if err := sess.Send(ctx, "hello"); err != nil {
		t.Fatalf("sess.Send: %v", err)
	}

	var gotComplete bool
	timeout := time.After(5 * time.Second)
loop:
	for {
		select {
		case <-timeout:
			t.Fatal("timed out waiting for CompleteEvent")
		case e, ok := <-eventCh:
			if !ok {
				break loop
			}
			if e.Kind == app.EventComplete {
				gotComplete = true
				break loop
			}
		}
	}
	if !gotComplete {
		t.Error("expected EventComplete from session")
	}
}

// ── Phase 3 tests ─────────────────────────────────────────────────────────────

func TestGetHistory_Empty(t *testing.T) {
	prov := newTestProvider("stub", "stub-model", "hi")
	close(prov.mdl.block)
	h := newTestHandler(t, prov)
	_, client := newTestServer(t, h)

	resp, err := client.GetHistory(context.Background(), connect.NewRequest(&feinov1.GetHistoryRequest{}))
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(resp.Msg.GetMessages()) != 0 {
		t.Errorf("expected empty history, got %d messages", len(resp.Msg.GetMessages()))
	}
}

func TestGetHistory_AfterSend(t *testing.T) {
	prov := newTestProvider("stub", "stub-model", "the answer")
	close(prov.mdl.block)
	h := newTestHandler(t, prov)
	_, client := newTestServer(t, h)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Complete a full turn so history has entries.
	stream, err := client.SendMessage(ctx, connect.NewRequest(&feinov1.SendMessageRequest{Text: "question"}))
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	for stream.Receive() {
	}
	if strErr := stream.Err(); strErr != nil {
		t.Fatalf("stream: %v", strErr)
	}

	resp, err := client.GetHistory(context.Background(), connect.NewRequest(&feinov1.GetHistoryRequest{}))
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(resp.Msg.GetMessages()) == 0 {
		t.Error("expected non-empty history after turn")
	}
}

func TestResetSession(t *testing.T) {
	prov := newTestProvider("stub", "stub-model", "reply")
	close(prov.mdl.block)
	h := newTestHandler(t, prov)
	_, client := newTestServer(t, h)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, _ := client.SendMessage(ctx, connect.NewRequest(&feinov1.SendMessageRequest{Text: "hi"}))
	for stream.Receive() {
	}

	_, err := client.ResetSession(context.Background(), connect.NewRequest(&feinov1.ResetSessionRequest{}))
	if err != nil {
		t.Fatalf("ResetSession: %v", err)
	}

	resp, err := client.GetHistory(context.Background(), connect.NewRequest(&feinov1.GetHistoryRequest{}))
	if err != nil {
		t.Fatalf("GetHistory after reset: %v", err)
	}
	if len(resp.Msg.GetMessages()) != 0 {
		t.Errorf("expected empty history after reset, got %d", len(resp.Msg.GetMessages()))
	}
}

func TestGetConfig_ReturnsConfig(t *testing.T) {
	prov := newTestProvider("stub", "stub-model", "hi")
	close(prov.mdl.block)
	h := newTestHandler(t, prov)
	_, client := newTestServer(t, h)

	resp, err := client.GetConfig(context.Background(), connect.NewRequest(&feinov1.GetConfigRequest{}))
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if resp.Msg.GetConfig() == nil {
		t.Error("expected non-nil config")
	}
}

func TestGetConfigYAML_ReturnsYAML(t *testing.T) {
	prov := newTestProvider("stub", "stub-model", "hi")
	close(prov.mdl.block)
	h := newTestHandler(t, prov)
	_, client := newTestServer(t, h)

	resp, err := client.GetConfigYAML(context.Background(), connect.NewRequest(&feinov1.GetConfigYAMLRequest{}))
	if err != nil {
		t.Fatalf("GetConfigYAML: %v", err)
	}
	if resp.Msg.GetYaml() == "" {
		t.Error("expected non-empty YAML")
	}
}

func TestBypassMode_RoundTrip(t *testing.T) {
	prov := newTestProvider("stub", "stub-model", "hi")
	close(prov.mdl.block)
	h := newTestHandler(t, prov)
	_, client := newTestServer(t, h)

	// Initially inactive.
	state, err := client.GetBypassState(context.Background(), connect.NewRequest(&feinov1.GetBypassStateRequest{}))
	if err != nil {
		t.Fatalf("GetBypassState: %v", err)
	}
	if state.Msg.GetActive() {
		t.Error("expected bypass inactive initially")
	}

	// Activate.
	_, err = client.SetBypassMode(context.Background(), connect.NewRequest(&feinov1.SetBypassModeRequest{SessionLong: true}))
	if err != nil {
		t.Fatalf("SetBypassMode: %v", err)
	}

	state, err = client.GetBypassState(context.Background(), connect.NewRequest(&feinov1.GetBypassStateRequest{}))
	if err != nil {
		t.Fatalf("GetBypassState: %v", err)
	}
	if !state.Msg.GetActive() {
		t.Error("expected bypass active after SetBypassMode")
	}

	// Deactivate.
	_, err = client.ClearBypassMode(context.Background(), connect.NewRequest(&feinov1.ClearBypassModeRequest{}))
	if err != nil {
		t.Fatalf("ClearBypassMode: %v", err)
	}

	state, err = client.GetBypassState(context.Background(), connect.NewRequest(&feinov1.GetBypassStateRequest{}))
	if err != nil {
		t.Fatalf("GetBypassState: %v", err)
	}
	if state.Msg.GetActive() {
		t.Error("expected bypass inactive after ClearBypassMode")
	}
}

func TestUploadFile_StoresContent(t *testing.T) {
	prov := newTestProvider("stub", "stub-model", "hi")
	close(prov.mdl.block)
	h := newTestHandler(t, prov)
	_, client := newTestServer(t, h)

	content := []byte("hello world file content")
	resp, err := client.UploadFile(context.Background(), connect.NewRequest(&feinov1.UploadFileRequest{
		Filename: "test.txt",
		Content:  content,
	}))
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if resp.Msg.GetToken() == "" {
		t.Error("expected non-empty upload token")
	}
	if resp.Msg.GetSizeBytes() != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), resp.Msg.GetSizeBytes())
	}
}

func TestListFiles_ReturnsEntries(t *testing.T) {
	prov := newTestProvider("stub", "stub-model", "hi")
	close(prov.mdl.block)
	h := newTestHandler(t, prov)
	_, client := newTestServer(t, h)

	resp, err := client.ListFiles(context.Background(), connect.NewRequest(&feinov1.ListFilesRequest{}))
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if resp.Msg.GetBase() == "" {
		t.Error("expected non-empty base path")
	}
}

func TestSetTheme_Persists(t *testing.T) {
	prov := newTestProvider("stub", "stub-model", "hi")
	close(prov.mdl.block)
	h := newTestHandler(t, prov)
	_, client := newTestServer(t, h)

	_, err := client.SetTheme(context.Background(), connect.NewRequest(&feinov1.SetThemeRequest{Theme: "dark"}))
	if err != nil {
		t.Fatalf("SetTheme: %v", err)
	}

	cfg, err := client.GetConfig(context.Background(), connect.NewRequest(&feinov1.GetConfigRequest{}))
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if got := cfg.Msg.GetConfig().GetUi().GetTheme(); got != "dark" {
		t.Errorf("expected theme=dark, got %q", got)
	}
}
