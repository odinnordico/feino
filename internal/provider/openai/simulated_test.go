package openai

import (
	"context"
	"strings"
	"testing"

	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/structs"
	"github.com/odinnordico/feino/internal/testserver"
)

// TestOpenAIProvider_WithSimulatedServer verifies that the real OpenAI provider
// and SDK are wire-compatible with the SimulatedServer: a matching prompt
// returns the recorded text and correct token counts via model.Model.Infer.
func TestOpenAIProvider_WithSimulatedServer(t *testing.T) {
	srv := testserver.NewSimulatedServer([]testserver.Record{
		{
			Prompt: "what is the capital of France",
			Response: testserver.RecordedResponse{
				Text:  "Paris",
				Usage: testserver.Usage{PromptTokens: 12, CompletionTokens: 3},
			},
		},
	})
	defer srv.Close()

	ctx := context.Background()
	p, err := NewProvider(ctx, "test-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Point the provider at the simulated server.
	p.baseURL = srv.URL() + "/v1/"
	p.client = p.createClient()

	m := &Model{
		id:       "simulated-model",
		provider: p,
		logger:   p.logger,
	}

	content := structs.NewLinkedList[model.MessagePart]()
	content.PushBack(model.NewTextMessagePart(model.MessageRoleUser, "what is the capital of France"))
	history := []model.Message{
		model.NewMessage(model.WithRole(model.MessageRoleUser), model.WithContent(content)),
	}

	var streamedText strings.Builder
	onPart := func(part model.MessagePart) {
		if txt, ok := part.GetContent().(string); ok {
			streamedText.WriteString(txt)
		}
	}

	msg, usage, err := m.Infer(ctx, history, model.InferOptions{}, onPart)
	if err != nil {
		t.Fatalf("Infer failed: %v", err)
	}

	// Verify streaming callback received text.
	st := streamedText.String()
	if st != "Paris" {
		t.Errorf("streaming callback: expected %q, got %q", "Paris", st)
	}

	// Verify final message content.
	var fullText strings.Builder
	for p := range msg.GetParts().Iterator() {
		if txt, ok := p.GetContent().(string); ok {
			fullText.WriteString(txt)
		}
	}
	if fullText.String() != "Paris" {
		t.Errorf("message content: expected %q, got %q", "Paris", fullText.String())
	}

	// Verify token counts from the simulated server's usage chunk.
	if usage.PromptTokens != 12 {
		t.Errorf("expected PromptTokens=12, got %d", usage.PromptTokens)
	}
	if usage.CompletionTokens != 3 {
		t.Errorf("expected CompletionTokens=3, got %d", usage.CompletionTokens)
	}
	if usage.TotalTokens != 15 {
		t.Errorf("expected TotalTokens=15, got %d", usage.TotalTokens)
	}
}

// TestOpenAIProvider_WithSimulatedServer_ToolCall verifies that tool call
// responses from the SimulatedServer are correctly parsed into model.ToolCall
// parts by the real OpenAI SDK streaming parser.
func TestOpenAIProvider_WithSimulatedServer_ToolCall(t *testing.T) {
	srv := testserver.NewSimulatedServer([]testserver.Record{
		{
			Prompt: "read the file at /tmp/data.txt",
			Response: testserver.RecordedResponse{
				ToolCalls: []testserver.ToolCall{{
					ID:        "call_abc",
					Name:      "file_read",
					Arguments: `{"path":"/tmp/data.txt"}`,
				}},
			},
		},
	})
	defer srv.Close()

	ctx := context.Background()
	p, err := NewProvider(ctx, "test-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.baseURL = srv.URL() + "/v1/"
	p.client = p.createClient()

	m := &Model{
		id:       "simulated-model",
		provider: p,
		logger:   p.logger,
	}

	content := structs.NewLinkedList[model.MessagePart]()
	content.PushBack(model.NewTextMessagePart(model.MessageRoleUser, "read the file at /tmp/data.txt"))
	history := []model.Message{
		model.NewMessage(model.WithRole(model.MessageRoleUser), model.WithContent(content)),
	}

	msg, _, err := m.Infer(ctx, history, model.InferOptions{}, nil)
	if err != nil {
		t.Fatalf("Infer failed: %v", err)
	}

	var toolCall model.ToolCall
	var found bool
	for part := range msg.GetParts().Iterator() {
		if tc, ok := part.GetContent().(model.ToolCall); ok {
			toolCall = tc
			found = true
		}
	}
	if !found {
		t.Fatal("expected a ToolCall part in the response message")
	}
	if toolCall.Name != "file_read" {
		t.Errorf("expected tool name 'file_read', got %q", toolCall.Name)
	}
	if toolCall.Arguments != `{"path":"/tmp/data.txt"}` {
		t.Errorf("expected arguments %q, got %q", `{"path":"/tmp/data.txt"}`, toolCall.Arguments)
	}
	if toolCall.ID != "call_abc" {
		t.Errorf("expected ID 'call_abc', got %q", toolCall.ID)
	}
}
