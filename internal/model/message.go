package model

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/odinnordico/feino/internal/structs"
)

type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleSystem    MessageRole = "system"
	MessageRoleTool      MessageRole = "tool"
)

// MessagePart represents a single component of a multi-modal message.
type MessagePart interface {
	GetRole() MessageRole
	GetContent() any
	GetTimestamp() string
	GetMetadata() map[string]any
}

// ToolCall represents a structured requested to execute a client-side tool.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string // JSON-serialized
}

// ToolResult represents the output of a tool execution.
type ToolResult struct {
	CallID  string
	Name    string
	Content string
	IsError bool
}

// Message is the primary data structure for conversational state.
type Message interface {
	GetID() string
	GetParts() *structs.LinkedList[MessagePart]
	GetTimestamp() string
	GetMetadata() map[string]any
	GetRole() MessageRole

	// GetTextContent flattens all text-based parts into a single string.
	GetTextContent() string
}

type interactionMessage struct {
	id        string
	role      MessageRole
	content   *structs.LinkedList[MessagePart]
	timestamp string
	metadata  map[string]any
}

func (m *interactionMessage) GetID() string {
	return m.id
}

func (m *interactionMessage) GetRole() MessageRole {
	return m.role
}

func (m *interactionMessage) GetParts() *structs.LinkedList[MessagePart] {
	return m.content
}

func (m *interactionMessage) GetTimestamp() string {
	return m.timestamp
}

func (m *interactionMessage) GetMetadata() map[string]any {
	return m.metadata
}

// GetTextContent traverses the linked list of parts to assemble a cohesive string.
func (m *interactionMessage) GetTextContent() string {
	if m.content == nil {
		return ""
	}

	var sb strings.Builder
	m.content.ForEach(func(part MessagePart) {
		if content, ok := part.GetContent().(string); ok {
			sb.WriteString(content)
		}
	})

	return sb.String()
}

type MessageOption func(*interactionMessage)

func WithRole(role MessageRole) MessageOption {
	return func(m *interactionMessage) {
		m.role = role
	}
}

func WithContent(content *structs.LinkedList[MessagePart]) MessageOption {
	return func(m *interactionMessage) {
		m.content = content
	}
}

func WithTimestamp(timestamp string) MessageOption {
	return func(m *interactionMessage) {
		m.timestamp = timestamp
	}
}

func WithMetadata(metadata map[string]any) MessageOption {
	return func(m *interactionMessage) {
		m.metadata = metadata
	}
}

func NewMessage(opts ...MessageOption) Message {
	msg := &interactionMessage{
		id:        uuid.New().String(),
		content:   structs.NewLinkedList[MessagePart](),
		timestamp: time.Now().Format(time.RFC3339),
	}
	for _, opt := range opts {
		opt(msg)
	}
	return msg
}
