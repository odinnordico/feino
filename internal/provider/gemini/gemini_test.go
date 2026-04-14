package gemini

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

func TestGeminiProvider_GetModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"models": [{"name": "models/gemini-3.1-pro", "displayName": "Gemini 3.1 Pro", "description": "Latest Pro model"}]}`)
	}))
	defer server.Close()

	ctx := context.Background()
	p, err := NewProvider(ctx, Config{APIKey: "test-key"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.baseURL = server.URL
	p.client, _ = p.createClient(ctx)

	models, err := p.GetModels(ctx)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if len(models) != 1 {
		t.Errorf("expected 1 model, got %d", len(models))
	}
	if models[0].GetID() != "models/gemini-3.1-pro" {
		t.Errorf("expected models/gemini-3.1-pro, got %s", models[0].GetID())
	}
}

func TestGeminiModel_Infer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Gemini pseudo-SSE is just NDJSON or similar, the SDK handles it.
		// For GenerateContentStream, it expects a sequence of JSON objects.
		_, _ = fmt.Fprint(w, "data: {\"candidates\": [{\"content\": {\"parts\": [{\"text\": \"Hello\"}]}}], \"usageMetadata\": {\"promptTokenCount\": 5, \"candidatesTokenCount\": 1, \"totalTokenCount\": 6}}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"candidates\": [{\"content\": {\"parts\": [{\"text\": \" world\"}]}}], \"usageMetadata\": {\"promptTokenCount\": 5, \"candidatesTokenCount\": 2, \"totalTokenCount\": 7}}\n\n")
	}))
	defer server.Close()

	ctx := context.Background()
	p, err := NewProvider(ctx, Config{APIKey: "test-key"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.baseURL = server.URL
	p.client, _ = p.createClient(ctx)

	m := &Model{
		id:       "models/gemini-3.1-pro",
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

	if usage.TotalTokens != 7 {
		t.Errorf("expected 7 total tokens, got %d", usage.TotalTokens)
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
}
