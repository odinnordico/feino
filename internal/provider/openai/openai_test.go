package openai

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

func TestOpenAIProvider_GetModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("expected path /v1/models, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"data": [{"id": "gpt-4o"}]}`)
	}))
	defer server.Close()

	ctx := context.Background()
	p, err := NewProvider(ctx, "test-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.baseURL = server.URL + "/v1/"
	p.client = p.createClient()

	models, err := p.GetModels(ctx)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].GetID() != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %s", models[0].GetID())
	}
}

func TestOpenAIModel_Infer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("expected path /v1/chat/completions, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")

		// Emit standard SSE SSE format for OpenAI
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-123\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-123\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"}}]}\n\n")
		// Usage chunk (choices is empty)
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-123\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	ctx := context.Background()
	p, err := NewProvider(ctx, "test-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.baseURL = server.URL + "/v1/"
	p.client = p.createClient()

	m := &Model{
		id:       "gpt-4o",
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
		t.Fatalf("expected nil error, got %v", err)
	}

	if usage.TotalTokens != 15 {
		t.Errorf("expected 15 total tokens, got %d", usage.TotalTokens)
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
