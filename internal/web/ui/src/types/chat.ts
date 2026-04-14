export type MessageRole = "user" | "assistant" | "system" | "error";

export interface TextPart {
  kind: "text";
  text: string;
}

export interface ThoughtPart {
  kind: "thought";
  text: string;
}

export interface ToolCallPart {
  kind: "tool_call";
  callId: string;
  name: string;
  arguments: string;
  result?: string;
  isError?: boolean;
}

export type MessagePart = TextPart | ThoughtPart | ToolCallPart;

export interface RenderedMessage {
  id: string;
  role: MessageRole;
  parts: MessagePart[];
  timestamp: number;
}

export interface PendingPermission {
  requestId: string;
  toolName: string;
  required: string;
  allowed: string;
}
