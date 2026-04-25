import { useToastStore, type Toast, type ToastKind } from "../../store/toastStore";

const ICONS: Record<ToastKind, string> = {
  success: "✓",
  error:   "✕",
  info:    "ℹ",
  warning: "⚠",
};

const COLORS: Record<ToastKind, string> = {
  success: "var(--color-primary)",
  error:   "var(--color-error)",
  info:    "var(--color-accent)",
  warning: "var(--color-yolo)",
};

function ToastItem({ t }: { t: Toast }) {
  const dismiss = useToastStore((s) => s.dismiss);
  return (
    <div
      role="alert"
      aria-live="assertive"
      style={{
        display: "flex",
        alignItems: "flex-start",
        gap: "10px",
        padding: "12px 16px",
        background: "var(--color-surface-2)",
        border: `1px solid ${COLORS[t.kind]}`,
        borderLeft: `3px solid ${COLORS[t.kind]}`,
        borderRadius: "var(--radius-md)",
        boxShadow: `0 4px 24px rgba(0,0,0,0.4), 0 0 8px ${COLORS[t.kind]}33`,
        fontFamily: "var(--font-sans)",
        fontSize: "var(--font-size-sm)",
        color: "var(--color-text)",
        minWidth: "260px",
        maxWidth: "420px",
        animation: "fadeSlideUp var(--transition-base) ease both",
        cursor: "default",
      }}
    >
      <span style={{ color: COLORS[t.kind], fontFamily: "var(--font-mono)", fontSize: "1rem", lineHeight: 1.4 }}>
        {ICONS[t.kind]}
      </span>
      <span style={{ flex: 1, lineHeight: 1.5 }}>{t.message}</span>
      <button
        onClick={() => dismiss(t.id)}
        aria-label="Dismiss notification"
        style={{
          background: "none",
          border: "none",
          cursor: "pointer",
          color: "var(--color-text-dim)",
          fontSize: "1rem",
          lineHeight: 1,
          padding: "0 2px",
        }}
      >
        ×
      </button>
    </div>
  );
}

export function ToastContainer() {
  const toasts = useToastStore((s) => s.toasts);
  if (toasts.length === 0) {return null;}

  return (
    <div
      aria-label="Notifications"
      style={{
        position: "fixed",
        bottom: "24px",
        right: "24px",
        zIndex: 9999,
        display: "flex",
        flexDirection: "column",
        gap: "8px",
        pointerEvents: "none",
      }}
    >
      {toasts.map((t) => (
        <div key={t.id} style={{ pointerEvents: "auto" }}>
          <ToastItem t={t} />
        </div>
      ))}
    </div>
  );
}
