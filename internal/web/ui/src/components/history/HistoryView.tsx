import { useEffect, useState } from "react";
import { useHistory } from "../../hooks/useHistory";
import { Button } from "../shared/Button";
import { Badge } from "../shared/Badge";
import { SkeletonCard } from "../shared/Skeleton";
import { EmptyState } from "../shared/EmptyState";
import { Modal } from "../shared/Modal";

function formatTime(ts: { seconds: bigint } | undefined): string {
  if (!ts) return "";
  return new Date(Number(ts.seconds) * 1000).toLocaleString();
}

export function HistoryView() {
  const { messages, loading, loadHistory, resetSession } = useHistory();
  const [confirmOpen, setConfirmOpen] = useState(false);

  useEffect(() => { loadHistory(); }, [loadHistory]);

  return (
    <div style={{ display: "flex", flexDirection: "column", height: "100%", padding: "24px", gap: "16px" }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
        <h1 style={{ color: "var(--color-primary)", fontFamily: "var(--font-mono)", fontSize: "var(--font-size-lg)", margin: 0 }}>
          History
        </h1>
        <Button
          variant="danger"
          onClick={() => setConfirmOpen(true)}
        >
          Reset Session
        </Button>
      </div>

      {confirmOpen && (
        <Modal title="Reset Session" onClose={() => setConfirmOpen(false)}>
          <p style={{ color: "var(--color-text)", fontSize: "var(--font-size-sm)", marginBottom: "16px" }}>
            Clear all conversation history?
          </p>
          <div style={{ display: "flex", gap: "8px", justifyContent: "flex-end" }}>
            <Button variant="ghost" onClick={() => setConfirmOpen(false)}>Cancel</Button>
            <Button variant="danger" onClick={() => { resetSession(); setConfirmOpen(false); }}>Yes, Reset</Button>
          </div>
        </Modal>
      )}

      {loading && (
        <div style={{ display: "flex", flexDirection: "column", gap: "12px" }}>
          {[80, 60, 100, 70].map((h, i) => <SkeletonCard key={i} height={`${h}px`} />)}
        </div>
      )}

      {!loading && messages.length === 0 && (
        <EmptyState
          icon="⊟"
          title="No conversation history"
          description="Start a conversation in Chat and it will appear here."
        />
      )}

      <div style={{ flex: 1, overflowY: "auto", display: "flex", flexDirection: "column", gap: "12px" }}>
        {messages.map((msg, idx) => (
          <div
            key={`${msg.role}-${idx}`}
            style={{
              background: "var(--color-surface-1)",
              border: "1px solid var(--color-border)",
              borderRadius: "var(--radius-md)",
              padding: "12px 16px",
            }}
          >
            <div style={{ display: "flex", alignItems: "center", gap: "8px", marginBottom: "8px" }}>
              <Badge variant={msg.role === "user" ? "primary" : msg.role === "assistant" ? "accent" : "dim"}>
                {msg.role}
              </Badge>
              <span style={{ color: "var(--color-text-faint)", fontSize: "var(--font-size-xs)", fontFamily: "var(--font-mono)" }}>
                {formatTime(msg.createdAt)}
              </span>
            </div>
            {msg.parts.map((part, pi) => {
              const c = part.content;
              if (!c.case) return null;
              if (c.case === "text") return (
                <div key={`${pi}-text`} style={{ color: "var(--color-text)", fontSize: "var(--font-size-sm)", whiteSpace: "pre-wrap" }}>
                  {c.value}
                </div>
              );
              if (c.case === "thought") return (
                <div key={`${pi}-thought`} style={{ color: "var(--color-thinking)", fontSize: "var(--font-size-xs)", fontStyle: "italic" }}>
                  💭 {c.value}
                </div>
              );
              if (c.case === "toolCall") return (
                <div key={`${pi}-toolCall`} style={{ color: "var(--color-tool)", fontSize: "var(--font-size-xs)", fontFamily: "var(--font-mono)" }}>
                  ⚙ {c.value.name}({c.value.arguments})
                </div>
              );
              return null;
            })}
          </div>
        ))}
      </div>
    </div>
  );
}
