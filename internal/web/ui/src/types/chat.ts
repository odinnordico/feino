export type MessageRole = "user" | "assistant" | "system" | "error";

export type TextPart = {
  kind: "text";
  text: string;
}

export type ThoughtPart = {
  kind: "thought";
  text: string;
}

export type ToolCallPart = {
  kind: "tool_call";
  callId: string;
  name: string;
  arguments: string;
  result?: string;
  isError?: boolean;
}

export type MessagePart = TextPart | ThoughtPart | ToolCallPart;

export type RenderedMessage = {
  id: string;
  role: MessageRole;
  parts: MessagePart[];
  timestamp: number;
}

export type PendingPermission = {
  requestId: string;
  toolName: string;
  required: string;
  allowed: string;
}
