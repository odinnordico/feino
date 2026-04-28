package ollama

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/provider"
	"github.com/odinnordico/feino/internal/structs"
)

// newTestProvider creates a Provider wired to the given httptest server URL,
// bypassing the ollama binary check entirely and sets up the client.
func newTestProvider(ctx context.Context, t *testing.T, serverURL string) *Provider {
	t.Helper()
	p := &Provider{
		logger:         slog.Default(),
		retryConfig:    provider.DefaultRetryConfig(),
		circuitBreaker: provider.DefaultCircuitBreaker(slog.Default()),
		baseURL:        serverURL,
		isTesting:      true,
	}
	client, err := p.createAndEnsureClient(ctx)
	if err != nil {
		t.Fatalf("createAndEnsureClient failed: %v", err)
	}
	p.client = client
	return p
}

func TestOllamaProvider_GetModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"models": [{"name": "llama3"}]}`)
	}))
	defer server.Close()

	ctx := context.Background()
	p := newTestProvider(ctx, t, server.URL)

	models, err := p.GetModels(ctx)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if len(models) != 1 {
		t.Errorf("expected 1 model, got %d", len(models))
	}
	if models[0].GetID() != "llama3" {
		t.Errorf("expected llama3, got %s", models[0].GetID())
	}
}

func TestOllamaModel_Infer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		// Ollama uses NDJSON for streaming
		_, _ = fmt.Fprintln(w, `{"message": {"content": "Hello"}, "done": false}`)
		_, _ = fmt.Fprintln(w, `{"message": {"content": " world"}, "done": true, "prompt_eval_count": 5, "eval_count": 2, "total_duration": 1000000}`)
	}))
	defer server.Close()

	ctx := context.Background()
	p := newTestProvider(ctx, t, server.URL)

	m := &Model{
		id:       "llama3",
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
