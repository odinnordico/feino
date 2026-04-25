import { useEffect, useState } from "react";
import { useChatStore } from "../../store/chatStore";
import { useMetricsStore } from "../../store/metricsStore";
import { useSessionStore } from "../../store/sessionStore";
import { formatMs, formatTokens } from "../../lib/utils";

function YoloCountdown({ expiry }: { expiry: number | null }) {
  const [remaining, setRemaining] = useState("");

  useEffect(() => {
    if (!expiry) { setRemaining("session"); return; }
    const expiryMs = expiry;

    function update() {
      const diff = expiryMs - Date.now();
      if (diff <= 0) { setRemaining("expired"); return; }
      const m = Math.floor(diff / 60000);
      const s = Math.floor((diff % 60000) / 1000);
      setRemaining(`${m}:${s.toString().padStart(2, "0")}`);
    }

    update();
    const id = setInterval(update, 1000);
    return () => clearInterval(id);
  }, [expiry]);

  return <span style={{ color: "var(--color-yolo)" }}>⚡ YOLO {remaining}</span>;
}

const stateColor: Record<string, string> = {
  idle:     "var(--color-text-faint)",
  init:     "var(--color-text-faint)",
  gather:   "var(--color-text-dim)",
  act:      "var(--color-accent)",
  verify:   "var(--color-accent)",
  complete: "var(--color-primary)",
  failed:   "var(--color-error)",
};

export function StatusBar() {
  const agentState  = useChatStore((s) => s.agentState);
  const usage       = useMetricsStore((s) => s.currentUsage);
  const { bypassActive, bypassExpiry } = useSessionStore();

  const color = stateColor[agentState] ?? "var(--color-text-dim)";

  return (
    <div
      style={{
        padding: "3px 16px",
        borderTop: "1px solid var(--color-border-dim)",
        background: "var(--color-surface-1)",
        display: "flex",
        gap: "14px",
        alignItems: "center",
        fontFamily: "var(--font-mono)",
        fontSize: "var(--font-size-xs)",
        color: "var(--color-text-faint)",
      }}
    >
      <span style={{ display: "flex", alignItems: "center", gap: "5px" }}>
        <span style={{ width: "6px", height: "6px", borderRadius: "50%", background: color, display: "inline-block" }} />
        <span style={{ color }}>{agentState}</span>
      </span>

      {usage && (
        <>
          <span>·</span>
          <span>Latency: <span style={{ color: "var(--color-text-dim)" }}>{formatMs(usage.durationMs)}</span></span>
          <span>·</span>
          <span>
            Turn:{" "}
            <span style={{ color: "var(--color-text-dim)" }}>
              {formatTokens(usage.promptTokens)}p / {formatTokens(usage.completionTokens)}c
            </span>
          </span>
          <span>·</span>
          <span style={{ color: "var(--color-primary)" }}>{formatTokens(usage.totalTokens)} total</span>
        </>
      )}

      {bypassActive && (
        <>
          <span>·</span>
          <YoloCountdown expiry={bypassExpiry} />
        </>
      )}
    </div>
  );
}
