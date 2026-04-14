package model

import "time"

// basePart holds the fields shared by every MessagePart implementation.
type basePart struct {
	role      MessageRole
	timestamp string
	metadata  map[string]any
}

func newBasePart(role MessageRole) basePart {
	return basePart{role: role, timestamp: time.Now().Format(time.RFC3339)}
}

func (b *basePart) GetRole() MessageRole        { return b.role }
func (b *basePart) GetTimestamp() string        { return b.timestamp }
func (b *basePart) GetMetadata() map[string]any { return b.metadata }

// TextMessagePart is the standard string-based component of a message.
type TextMessagePart struct {
	basePart
	content string
}

func NewTextMessagePart(role MessageRole, content string) *TextMessagePart {
	return &TextMessagePart{basePart: newBasePart(role), content: content}
}

func (p *TextMessagePart) GetContent() any { return p.content }

// ToolCallPart encapsulates a structured tool execution request from the model.
type ToolCallPart struct {
	basePart
	call ToolCall
}

func NewToolCallPart(call ToolCall) *ToolCallPart {
	return &ToolCallPart{basePart: newBasePart(MessageRoleAssistant), call: call}
}

func (p *ToolCallPart) GetContent() any { return p.call }

// ToolResultPart encapsulates the output of a tool execution for feedback to the model.
type ToolResultPart struct {
	basePart
	result ToolResult
}

func NewToolResultPart(result ToolResult) *ToolResultPart {
	return &ToolResultPart{basePart: newBasePart(MessageRoleTool), result: result}
}

func (p *ToolResultPart) GetContent() any { return p.result }

// ThoughtPart encapsulates the model's internal reasoning or "thinking" process.
// It is distinct from TextMessagePart to allow specialised UI rendering.
type ThoughtPart struct {
	basePart
	content string
}

func NewThoughtPart(content string) *ThoughtPart {
	return &ThoughtPart{basePart: newBasePart(MessageRoleAssistant), content: content}
}

func (p *ThoughtPart) GetContent() any { return p.content }
