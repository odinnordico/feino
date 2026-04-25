import { useEffect, useRef } from "react";
import { useChatStore } from "../../store/chatStore";
import { MessageBubble } from "./MessageBubble";
import { MessageQueueBadge } from "./MessageQueue";
import { Spinner } from "../shared/Spinner";
import { EmptyState } from "../shared/EmptyState";

export function MessageList() {
  const messages = useChatStore((s) => s.messages);
  const busy     = useChatStore((s) => s.busy);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  // Only scroll when message count changes, not on content updates.
   
  }, [messages.length]);

  const lastAssistantIdx = messages.reduce(
    (acc, m, i) => (m.role === "assistant" ? i : acc),
    -1
  );

  return (
    <div
      role="log"
      aria-label="Conversation"
      aria-live="polite"
      style={{
        flex: 1,
        overflowY: "auto",
        padding: "20px 24px",
        display: "flex",
        flexDirection: "column",
      }}
    >
      {messages.length === 0 && !busy && (
        <div style={{ margin: "auto" }}>
          <EmptyState
            icon="◈"
            title="Start a conversation"
            description={`Type a message below or use / for commands`}
          />
        </div>
      )}

      {messages.map((msg, idx) => (
        <MessageBubble
          key={msg.id}
          msg={msg}
          isStreaming={busy && idx === lastAssistantIdx}
        />
      ))}

      <MessageQueueBadge />

      {busy && messages.length === 0 && (
        <div style={{ padding: "8px 0" }}>
          <Spinner />
        </div>
      )}

      <div ref={bottomRef} />
    </div>
  );
}
