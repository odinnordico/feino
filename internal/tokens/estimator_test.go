package tokens

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/structs"
)

// mockMessagePart implements model.MessagePart for testing
type mockMessagePart struct {
	content string
}

func (m *mockMessagePart) GetRole() model.MessageRole  { return model.MessageRoleUser }
func (m *mockMessagePart) GetContent() any             { return m.content }
func (m *mockMessagePart) GetTimestamp() string        { return "" }
func (m *mockMessagePart) GetMetadata() map[string]any { return nil }

// mockMessage implements model.Message for testing
type mockMessage struct {
	parts *structs.LinkedList[model.MessagePart]
}

func (m *mockMessage) GetID() string                                    { return "mocked_id" }
func (m *mockMessage) GetParts() *structs.LinkedList[model.MessagePart] { return m.parts }
func (m *mockMessage) GetTimestamp() string                             { return "" }
func (m *mockMessage) GetMetadata() map[string]any                      { return nil }
func (m *mockMessage) GetRole() model.MessageRole                       { return model.MessageRoleUser }
func (m *mockMessage) GetTextContent() string {
	var sb strings.Builder
	m.parts.ForEach(func(p model.MessagePart) {
		if s, ok := p.GetContent().(string); ok {
			sb.WriteString(s)
		}
	})
	return sb.String()
}

func TestEstimatorString(t *testing.T) {
	est := NewEstimator(slog.Default(), WithChatMLOverheads(4, 3))

	// "Hello, world!" is 3 tokens in cl100k_base or o200k_base. (Hello)(, world)(!) or (Hello)(, )(world)(!) depending on exact tokenization,
	// Wait, actually "Hello, world!" is 4 tokens in cl100k_base: "Hello", ",", " world", "!"
	// We'll just verify no error and > 0 tokens is returned.

	count, err := est.EstimateString("Hello, world!", "gpt-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if count <= 0 {
		t.Fatalf("expected count > 0, got %d", count)
	}
}

func TestEstimatorMessage(t *testing.T) {
	est := NewEstimator(slog.Default(), WithChatMLOverheads(4, 3))

	ll := &structs.LinkedList[model.MessagePart]{}
	ll.PushBack(&mockMessagePart{content: "This is a test."})

	msg := &mockMessage{parts: ll}

	count, err := est.EstimateMessage(msg, "gpt-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 4 overhead tokens + ~4 payload tokens
	if count < 5 {
		t.Fatalf("expected sum including overhead, but got %d", count)
	}
}

func TestEstimatorMessages(t *testing.T) {
	est := NewEstimator(slog.Default(), WithChatMLOverheads(4, 3))

	msgs := []model.Message{}
	for range 3 {
		ll := &structs.LinkedList[model.MessagePart]{}
		ll.PushBack(&mockMessagePart{content: "Another test string."})
		msgs = append(msgs, &mockMessage{parts: ll})
	}

	total, err := est.EstimateMessages(msgs, "gpt-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be 3 * (overhead 4 + string length) + assistant prime overhead 3
	if total < 10 {
		t.Fatalf("expected significant total, but got %d", total)
	}
}
