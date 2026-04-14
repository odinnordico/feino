package openaicompat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/structs"
)

// newTestProvider spins up a test HTTP server with the given handler, creates
// a Provider pointing at it, and returns both. The server is automatically
// registered for cleanup via t.Cleanup.
func newTestProvider(t *testing.T, h http.HandlerFunc, name string) (*Provider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	cfg := Config{
		BaseURL: srv.URL + "/v1",
		APIKey:  "test-key",
		Name:    name,
	}
	p, err := NewProvider(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	// Point the client at the test server directly.
	p.baseURL = srv.URL + "/v1/"
	p.client = p.createClient()
	return p, srv
}

// ── constructor ───────────────────────────────────────────────────────────────

func TestNewProvider_RequiresBaseURL(t *testing.T) {
	_, err := NewProvider(context.Background(), Config{}, nil)
	if err == nil {
		t.Fatal("expected error for empty BaseURL, got nil")
	}
}

func TestNewProvider_DefaultAPIKey(t *testing.T) {
	// When no key is in config, the client should still be constructed (using
	// the "none" fallback). We just verify the constructor does not error.
	cfg := Config{BaseURL: "http://localhost:8000/v1"}
	p, err := NewProvider(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

// ── identity ──────────────────────────────────────────────────────────────────

func TestProvider_Identity(t *testing.T) {
	cfg := Config{BaseURL: "http://localhost:8000/v1", Name: "My vLLM"}
	p, _ := NewProvider(context.Background(), cfg, nil)

	if p.GetID() != "openai_compat" {
		t.Errorf("GetID: got %q, want openai_compat", p.GetID())
	}
	if p.GetName() != "My vLLM" {
		t.Errorf("GetName: got %q, want My vLLM", p.GetName())
	}
}

func TestProvider_DefaultName(t *testing.T) {
	cfg := Config{BaseURL: "http://localhost:8000/v1"}
	p, _ := NewProvider(context.Background(), cfg, nil)
	if p.GetName() != "OpenAI-Compatible" {
		t.Errorf("GetName: got %q, want OpenAI-Compatible", p.GetName())
	}
}

// ── GetModels ─────────────────────────────────────────────────────────────────

func TestProvider_GetModels(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"data":[{"id":"mistralai/Mistral-7B-v0.1"},{"id":"llama-3-8b"}]}`)
	}, "vLLM")

	models, err := p.GetModels(context.Background())
	if err != nil {
		t.Fatalf("GetModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].GetID() != "mistralai/Mistral-7B-v0.1" {
		t.Errorf("model[0].GetID: got %q", models[0].GetID())
	}
	if models[1].GetID() != "llama-3-8b" {
		t.Errorf("model[1].GetID: got %q", models[1].GetID())
	}
}

func TestProvider_GetModels_APIError(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}, "vLLM")

	// Disable retries to keep the test fast.
	p.retryConfig.MaxRetries = 0

	_, err := p.GetModels(context.Background())
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
}

// ── Infer ─────────────────────────────────────────────────────────────────────

func singleUserHistory(text string) []model.Message {
	content := structs.NewLinkedList[model.MessagePart]()
	content.PushBack(model.NewTextMessagePart(model.MessageRoleUser, text))
	return []model.Message{
		model.NewMessage(model.WithRole(model.MessageRoleUser), model.WithContent(content)),
	}
}

func TestModel_Infer_Text(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"1\",\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}, "LocalAI")

	m := &Model{id: "mistral-7b", provider: p, logger: p.logger}

	var collected []string
	onPart := func(part model.MessagePart) {
		if txt, ok := part.GetContent().(string); ok {
			collected = append(collected, txt)
		}
	}

	msg, usage, err := m.Infer(context.Background(), singleUserHistory("hi"), model.InferOptions{}, onPart)
	if err != nil {
		t.Fatalf("Infer: %v", err)
	}
	if usage.TotalTokens != 7 {
		t.Errorf("TotalTokens: got %d, want 7", usage.TotalTokens)
	}

	var full strings.Builder
	for p := range msg.GetParts().Iterator() {
		if txt, ok := p.GetContent().(string); ok {
			full.WriteString(txt)
		}
	}
	if full.String() != "Hello world" {
		t.Errorf("full text: got %q, want %q", full.String(), "Hello world")
	}
	if len(collected) != 2 {
		t.Errorf("streaming parts: got %d, want 2", len(collected))
	}
}

func TestModel_Infer_ToolCall(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Two delta chunks build up a single tool call.
		_, _ = fmt.Fprint(w, `data: {"id":"2","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_abc","function":{"name":"file_read","arguments":""}}]}}]}`+"\n\n")
		_, _ = fmt.Fprint(w, `data: {"id":"2","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":\"/tmp/x\"}"}}]}}]}`+"\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}, "vLLM")

	m := &Model{id: "qwen-2.5", provider: p, logger: p.logger}

	var gotToolCall *model.ToolCall
	onPart := func(part model.MessagePart) {
		if tc, ok := part.GetContent().(model.ToolCall); ok {
			gotToolCall = &tc
		}
	}

	msg, _, err := m.Infer(context.Background(), singleUserHistory("read /tmp/x"), model.InferOptions{}, onPart)
	if err != nil {
		t.Fatalf("Infer: %v", err)
	}
	if gotToolCall == nil {
		t.Fatal("expected tool call via onPart, got nil")
	}
	if gotToolCall.Name != "file_read" {
		t.Errorf("tool name: got %q, want file_read", gotToolCall.Name)
	}
	if gotToolCall.ID != "call_abc" {
		t.Errorf("tool ID: got %q, want call_abc", gotToolCall.ID)
	}
	wantArgs := `{"path":"/tmp/x"}`
	if gotToolCall.Arguments != wantArgs {
		t.Errorf("tool arguments: got %q, want %q", gotToolCall.Arguments, wantArgs)
	}

	// The assembled message must also contain the tool call.
	var foundInMsg bool
	for p := range msg.GetParts().Iterator() {
		if tc, ok := p.GetContent().(model.ToolCall); ok {
			if tc.Name == "file_read" {
				foundInMsg = true
			}
		}
	}
	if !foundInMsg {
		t.Error("tool call not present in returned message parts")
	}
}

func TestModel_Infer_EmptyResponse(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Server sends no content — only DONE.
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}, "vLLM")
	p.retryConfig.MaxRetries = 0

	m := &Model{id: "some-model", provider: p, logger: p.logger}
	_, _, err := m.Infer(context.Background(), singleUserHistory("hi"), model.InferOptions{}, nil)
	if err == nil {
		t.Fatal("expected error on empty response, got nil")
	}
}

func TestModel_Infer_NilOnPart(t *testing.T) {
	// Passing nil for onPart must not panic.
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"3\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}, "vLLM")

	m := &Model{id: "llama3", provider: p, logger: p.logger}
	_, _, err := m.Infer(context.Background(), singleUserHistory("hi"), model.InferOptions{}, nil)
	if err != nil {
		t.Fatalf("Infer with nil onPart: %v", err)
	}
}

// ── renewClient ───────────────────────────────────────────────────────────────

func TestProvider_RenewClient_ReadsEnvKey(t *testing.T) {
	// Start with no key in config; set one in the environment.
	_ = os.Setenv("OPENAI_COMPAT_API_KEY", "env-key-abc")
	t.Cleanup(func() { _ = os.Unsetenv("OPENAI_COMPAT_API_KEY") })

	cfg := Config{BaseURL: "http://localhost:8000/v1"} // no key
	p, err := NewProvider(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	// Simulate key rotation: the provider originally had no key, now the env
	// has one — renewClient must pick it up.
	p.cfg.APIKey = "" // reset to empty as if originally unset
	if err := p.renewClient(context.Background()); err != nil {
		t.Fatalf("renewClient: %v", err)
	}

	if p.cfg.APIKey != "env-key-abc" {
		t.Errorf("APIKey after renewal: got %q, want env-key-abc", p.cfg.APIKey)
	}
	if p.client == nil {
		t.Error("client should not be nil after renewal")
	}
}

func TestProvider_RenewClient_KeepsExistingKeyWhenEnvEmpty(t *testing.T) {
	_ = os.Unsetenv("OPENAI_COMPAT_API_KEY")

	cfg := Config{BaseURL: "http://localhost:8000/v1", APIKey: "original-key"}
	p, err := NewProvider(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	if err := p.renewClient(context.Background()); err != nil {
		t.Fatalf("renewClient: %v", err)
	}

	// Env was empty — key must not change.
	if p.cfg.APIKey != "original-key" {
		t.Errorf("APIKey: got %q, want original-key", p.cfg.APIKey)
	}
}

// ── SetModel / GetSelectedModel ───────────────────────────────────────────────

func TestProvider_SetGetModel(t *testing.T) {
	cfg := Config{BaseURL: "http://localhost:8000/v1"}
	p, _ := NewProvider(context.Background(), cfg, nil)

	m := &Model{id: "mistral", provider: p, logger: p.logger}
	p.SetModel(m)

	got := p.GetSelectedModel()
	if got == nil || got.GetID() != "mistral" {
		t.Errorf("GetSelectedModel: got %v", got)
	}
}

// ── DisableTools ──────────────────────────────────────────────────────────────

func TestModel_SupportsTools_Default(t *testing.T) {
	cfg := Config{BaseURL: "http://localhost:8000/v1"}
	p, _ := NewProvider(context.Background(), cfg, nil)
	m := &Model{id: "llama3", provider: p, logger: p.logger}
	if !m.SupportsTools() {
		t.Error("SupportsTools: expected true when DisableTools is false")
	}
}

func TestModel_SupportsTools_Disabled(t *testing.T) {
	cfg := Config{BaseURL: "http://localhost:8000/v1", DisableTools: true}
	p, _ := NewProvider(context.Background(), cfg, nil)
	m := &Model{id: "llama-cpp", provider: p, logger: p.logger}
	if m.SupportsTools() {
		t.Error("SupportsTools: expected false when DisableTools is true")
	}
}
