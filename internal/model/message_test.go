package model

import (
	"strings"
	"testing"

	"github.com/odinnordico/feino/internal/structs"
)

func TestMessage_GetTextContent(t *testing.T) {
	msg := NewMessage(WithRole(MessageRoleAssistant))
	parts := msg.GetParts()

	parts.PushBack(NewTextMessagePart(MessageRoleAssistant, "Searching for files..."))
	parts.PushBack(NewTextMessagePart(MessageRoleAssistant, "\nFound 3 matches."))

	content := msg.GetTextContent()
	expected := "Searching for files...\nFound 3 matches."
	if content != expected {
		t.Errorf("Expected %q, got %q", expected, content)
	}
}

func TestMessage_GetTextContent_FiltersStructParts(t *testing.T) {
	msg := NewMessage(WithRole(MessageRoleAssistant))
	parts := msg.GetParts()

	parts.PushBack(NewTextMessagePart(MessageRoleAssistant, "before"))
	// ToolCallPart and ToolResultPart have struct content — excluded.
	parts.PushBack(NewToolCallPart(ToolCall{ID: "1", Name: "ls", Arguments: "{}"}))
	parts.PushBack(NewToolResultPart(ToolResult{CallID: "1", Content: "ok"}))
	parts.PushBack(NewTextMessagePart(MessageRoleAssistant, "after"))

	content := msg.GetTextContent()
	if content != "beforeafter" {
		t.Errorf("expected only text parts concatenated, got %q", content)
	}
}

func TestMessage_ToolParts(t *testing.T) {
	// Test ToolCallPart
	call := ToolCall{ID: "123", Name: "ls", Arguments: "{}"}
	tcp := NewToolCallPart(call)

	if tcp.GetRole() != MessageRoleAssistant {
		t.Errorf("Expected assistant role for tool call, got %s", tcp.GetRole())
	}
	if tcp.GetContent().(ToolCall).ID != "123" {
		t.Errorf("ToolCall ID mismatch")
	}

	// Test ToolResultPart
	res := ToolResult{CallID: "123", Content: "file1.go", IsError: false}
	trp := NewToolResultPart(res)

	if trp.GetRole() != MessageRoleTool {
		t.Errorf("Expected tool role for tool result, got %s", trp.GetRole())
	}
	if trp.GetContent().(ToolResult).Content != "file1.go" {
		t.Errorf("ToolResult content mismatch")
	}
}

func TestThoughtPart(t *testing.T) {
	tp := NewThoughtPart("deep reasoning")

	if tp.GetRole() != MessageRoleAssistant {
		t.Errorf("expected assistant role, got %s", tp.GetRole())
	}
	content, ok := tp.GetContent().(string)
	if !ok {
		t.Fatalf("expected string content, got %T", tp.GetContent())
	}
	if content != "deep reasoning" {
		t.Errorf("expected %q, got %q", "deep reasoning", content)
	}
	if tp.GetTimestamp() == "" {
		t.Error("timestamp should not be empty")
	}
}

func TestNewMessage_Defaults(t *testing.T) {
	msg := NewMessage()
	if msg.GetParts() == nil {
		t.Errorf("Expected non-nil linked list for parts")
	}
	if msg.GetParts().Size() != 0 {
		t.Errorf("Expected empty parts list, got size %d", msg.GetParts().Size())
	}
	if msg.GetID() == "" {
		t.Error("ID should not be empty")
	}
	if msg.GetTimestamp() == "" {
		t.Error("timestamp should not be empty")
	}
}

func TestNewMessage_UniqueIDs(t *testing.T) {
	seen := make(map[string]bool, 20)
	for i := range 20 {
		id := NewMessage().GetID()
		if seen[id] {
			t.Fatalf("duplicate ID %q on iteration %d", id, i)
		}
		seen[id] = true
	}
}

func TestMessageOptions(t *testing.T) {
	list := structs.NewLinkedList[MessagePart]()
	meta := map[string]any{"key": "value"}

	msg := NewMessage(
		WithRole(MessageRoleSystem),
		WithContent(list),
		WithTimestamp("2024-01-01T00:00:00Z"),
		WithMetadata(meta),
	)

	if msg.GetRole() != MessageRoleSystem {
		t.Errorf("role: want %q, got %q", MessageRoleSystem, msg.GetRole())
	}
	if msg.GetParts() != list {
		t.Error("parts list should be the one passed via WithContent")
	}
	if msg.GetTimestamp() != "2024-01-01T00:00:00Z" {
		t.Errorf("timestamp: want %q, got %q", "2024-01-01T00:00:00Z", msg.GetTimestamp())
	}
	if msg.GetMetadata()["key"] != "value" {
		t.Error("metadata not applied correctly")
	}
}

func TestMessage_EmptyParts_GetTextContent(t *testing.T) {
	msg := NewMessage()
	if msg.GetTextContent() != "" {
		t.Errorf("expected empty string for message with no parts, got %q", msg.GetTextContent())
	}
}

func TestBasePart_Timestamp(t *testing.T) {
	for _, part := range []MessagePart{
		NewTextMessagePart(MessageRoleUser, "hello"),
		NewToolCallPart(ToolCall{ID: "1", Name: "x", Arguments: "{}"}),
		NewToolResultPart(ToolResult{CallID: "1", Content: "ok"}),
		NewThoughtPart("thinking"),
	} {
		if part.GetTimestamp() == "" {
			t.Errorf("%T: timestamp should not be empty", part)
		}
		if !strings.Contains(part.GetTimestamp(), "T") {
			t.Errorf("%T: timestamp %q does not look like RFC3339", part, part.GetTimestamp())
		}
	}
}
