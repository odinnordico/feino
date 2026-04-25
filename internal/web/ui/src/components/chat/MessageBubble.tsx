import type { RenderedMessage, MessagePart } from "../../types/chat";
import { MarkdownRenderer } from "./MarkdownRenderer";
import { ThoughtBlock } from "./ThoughtBlock";
import { ToolCallCard } from "./ToolCallCard";
import { StreamingCursor } from "./StreamingCursor";

type MessageBubbleProps = {
  msg: RenderedMessage;
  isStreaming?: boolean;
}

function toolCallStatus(part: Extract<MessagePart, { kind: "tool_call" }>): "pending" | "running" | "resolved" | "error" {
  if (part.result === undefined) {return "pending";}
  return part.isError ? "error" : "resolved";
}

export function MessageBubble({ msg, isStreaming = false }: MessageBubbleProps) {
  if (msg.role === "user") {
    return (
      <div
        className="message-enter"
        style={{
          alignSelf: "flex-end",
          maxWidth: "75%",
          background: "var(--color-surface-2)",
          border: "1px solid var(--color-border)",
          borderRadius: "var(--radius-lg) var(--radius-lg) var(--radius-sm) var(--radius-lg)",
          padding: "10px 14px",
          marginBottom: "12px",
        }}
      >
        <div style={{ color: "var(--color-primary)", fontSize: "var(--font-size-xs)", fontFamily: "var(--font-mono)", marginBottom: "4px" }}>
          You
        </div>
        <div style={{ color: "var(--color-text)", whiteSpace: "pre-wrap", wordBreak: "break-word" }}>
          {msg.parts.map((p, i) => p.kind === "text" ? <span key={`${i}-text`}>{p.text}</span> : null)}
        </div>
      </div>
    );
  }

  if (msg.role === "error") {
    return (
      <div
        className="message-enter"
        style={{
          alignSelf: "stretch",
          background: "var(--color-error-muted)",
          border: "1px solid var(--color-error)",
          borderRadius: "var(--radius-md)",
          padding: "10px 14px",
          marginBottom: "12px",
          color: "var(--color-error)",
          fontSize: "var(--font-size-sm)",
        }}
      >
        ✕ {msg.parts.map((p, i) => p.kind === "text" ? <span key={`${i}-text`}>{p.text}</span> : null)}
      </div>
    );
  }

  if (msg.role === "system") {
    return (
      <div
        className="message-enter"
        style={{
          alignSelf: "center",
          background: "var(--color-surface-1)",
          border: "1px solid var(--color-border-dim)",
          borderRadius: "var(--radius-pill)",
          padding: "4px 16px",
          marginBottom: "12px",
          color: "var(--color-text-dim)",
          fontSize: "var(--font-size-xs)",
          fontStyle: "italic",
        }}
      >
        {msg.parts.map((p, i) => p.kind === "text" ? <span key={`${i}-text`}>{p.text}</span> : null)}
      </div>
    );
  }

  // assistant
  const textParts = msg.parts.filter((p) => p.kind === "text");
  const lastTextIdx = msg.parts.reduce((acc, p, i) => p.kind === "text" ? i : acc, -1);

  return (
    <div
      className="message-enter"
      style={{
        alignSelf: "flex-start",
        maxWidth: "85%",
        marginBottom: "16px",
      }}
    >
      <div style={{ color: "var(--color-accent)", fontSize: "var(--font-size-xs)", fontFamily: "var(--font-mono)", marginBottom: "6px" }}>
        FEINO
      </div>

      <div style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
        {msg.parts.map((part, i) => {
          if (part.kind === "thought") {
            return <ThoughtBlock key={`${i}-thought`} text={part.text} isStreaming={isStreaming} />;
          }
          if (part.kind === "tool_call") {
            return <ToolCallCard key={part.callId || `${i}-tool_call`} part={part} status={toolCallStatus(part)} />;
          }
          // text
          const isLast = i === lastTextIdx;
          return (
            <div key={`${i}-text`}>
              <MarkdownRenderer content={part.text} />
              {isLast && isStreaming && textParts.length > 0 && <StreamingCursor />}
            </div>
          );
        })}

        {isStreaming && textParts.length === 0 && <StreamingCursor />}
      </div>
    </div>
  );
}
