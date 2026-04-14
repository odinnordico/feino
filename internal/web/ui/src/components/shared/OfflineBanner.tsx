import { useSessionStore } from "../../store/sessionStore";

export function OfflineBanner() {
  const offline = useSessionStore((s) => s.offline);
  if (!offline) return null;

  return (
    <div
      role="status"
      aria-live="polite"
      style={{
        position: "fixed",
        top: 0,
        left: 0,
        right: 0,
        zIndex: 9998,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        gap: "10px",
        padding: "8px 16px",
        background: "var(--color-error)",
        color: "#000",
        fontFamily: "var(--font-mono)",
        fontSize: "var(--font-size-sm)",
        fontWeight: 600,
      }}
    >
      <span>⚠</span>
      <span>Cannot reach server — retrying…</span>
    </div>
  );
}
