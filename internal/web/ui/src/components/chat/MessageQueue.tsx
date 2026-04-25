import { useChatStore } from "../../store/chatStore";

/** Shows a queue position pill when the session is busy and this message is queued. */
export function MessageQueueBadge() {
  const pos = useChatStore((s) => s.queuePosition);
  if (pos === 0) {return null;}

  return (
    <div
      style={{
        alignSelf: "center",
        display: "inline-flex",
        alignItems: "center",
        gap: "6px",
        padding: "4px 14px",
        borderRadius: "var(--radius-pill)",
        background: "var(--color-warning-muted)",
        border: "1px solid var(--color-warning)",
        color: "var(--color-warning)",
        fontFamily: "var(--font-mono)",
        fontSize: "var(--font-size-xs)",
        marginBottom: "8px",
        animation: "fadeSlideUp var(--transition-base) ease both",
      }}
    >
      <span style={{ animation: "pulse-dot 1.4s ease-in-out infinite", display: "inline-block" }}>●</span>
      Queued — position {pos}
    </div>
  );
}
