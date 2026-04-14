package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/structs"
)

func TestAnthropicProvider_GetModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("expected path /v1/models, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"data": [{"id": "claude-3-opus-20240229", "display_name": "Claude 3 Opus", "max_tokens": 4096}], "has_more": false}`)
	}))
	defer server.Close()

	ctx := context.Background()
	p, err := NewProvider(ctx, "test-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.baseURL = server.URL + "/"
	p.client = p.createClient()

	models, err := p.GetModels(ctx)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if len(models) != 1 {
		t.Errorf("expected 1 model, got %d", len(models))
	}
	if models[0].GetID() != "claude-3-opus-20240229" {
		t.Errorf("expected claude-3-opus-20240229, got %s", models[0].GetID())
	}
}

func TestAnthropicModel_Infer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("expected path /v1/messages, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Mock SSE events
		_, _ = fmt.Fprint(w, "event: message_start\ndata: {\"type\": \"message_start\", \"message\": {\"id\": \"msg_123\", \"role\": \"assistant\", \"content\": [], \"usage\": {\"input_tokens\": 10, \"output_tokens\": 0}}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_start\ndata: {\"type\": \"content_block_start\", \"index\": 0, \"content_block\": {\"type\": \"text\", \"text\": \"\"}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\": \"content_block_delta\", \"index\": 0, \"delta\": {\"type\": \"text_delta\", \"text\": \"Hello\"}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\": \"content_block_delta\", \"index\": 0, \"delta\": {\"type\": \"text_delta\", \"text\": \" world\"}}\n\n")
		_, _ = fmt.Fprint(w, "event: message_delta\ndata: {\"type\": \"message_delta\", \"delta\": {\"stop_reason\": \"end_turn\"}, \"usage\": {\"output_tokens\": 5}}\n\n")
		_, _ = fmt.Fprint(w, "event: message_stop\ndata: {\"type\": \"message_stop\"}\n\n")
	}))
	defer server.Close()

	ctx := context.Background()
	p, err := NewProvider(ctx, "test-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.baseURL = server.URL + "/"
	p.client = p.createClient()

	m := &Model{
		id:       "claude-3-opus-20240229",
		provider: p,
		logger:   p.logger,
	}

	content := structs.NewLinkedList[model.MessagePart]()
	content.PushBack(model.NewTextMessagePart(model.MessageRoleUser, "hi"))
	history := []model.Message{
		model.NewMessage(model.WithRole(model.MessageRoleUser), model.WithContent(content)),
	}

	var parts []string
	onPart := func(p model.MessagePart) {
		if txt, ok := p.GetContent().(string); ok {
			parts = append(parts, txt)
		}
	}

	msg, usage, err := m.Infer(ctx, history, model.InferOptions{}, onPart)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}

	if usage.PromptTokens != 10 {
		t.Errorf("expected 10 prompt tokens, got %d", usage.PromptTokens)
	}
	if usage.CompletionTokens != 5 {
		t.Errorf("expected 5 completion tokens, got %d", usage.CompletionTokens)
	}

	var fullText strings.Builder
	for p := range msg.GetParts().Iterator() {
		if txt, ok := p.GetContent().(string); ok {
			fullText.WriteString(txt)
		}
	}

	if fullText.String() != "Hello world" {
		t.Errorf("expected 'Hello world', got '%s'", fullText.String())
	}
	if len(parts) != 2 {
		t.Errorf("expected 2 parts, got %d", len(parts))
	}
}
