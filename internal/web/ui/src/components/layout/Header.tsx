import { create } from "@bufbuild/protobuf";
import { feinoClient } from "../../client";
import { SetThemeRequestSchema } from "../../gen/feino/v1/feino_pb";
import { useChatStore } from "../../store/chatStore";
import { useSessionStore } from "../../store/sessionStore";
import { Badge } from "../shared/Badge";
import { Tooltip } from "../shared/Tooltip";

export function Header() {
  const busy        = useChatStore((s) => s.busy);
  const agentState  = useChatStore((s) => s.agentState);
  const { theme, setTheme, modelName, bypassActive } = useSessionStore();

  async function toggleTheme() {
    const next = theme === "dark" ? "light" : "dark";
    setTheme(next);
    try {
      await feinoClient.setTheme(create(SetThemeRequestSchema, { theme: next }));
    } catch { /* best effort */ }
  }

  const stateVariant = busy
    ? (agentState === "act" || agentState === "verify" ? "accent" : "dim")
    : "dim";

  return (
    <header
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        padding: "0 20px",
        height: "56px",
        background: "var(--color-surface-1)",
        borderBottom: "1px solid var(--color-border)",
        flexShrink: 0,
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: "12px" }}>
        <span style={{ color: "var(--color-primary)", fontWeight: 700, fontFamily: "var(--font-mono)", fontSize: "1.1rem", letterSpacing: "0.05em" }}>
          FEINO
        </span>
        {modelName && (
          <Badge variant="dim">{modelName}</Badge>
        )}
        {busy && (
          <Badge variant={stateVariant} pulse={stateVariant === "accent"}>
            {agentState}
          </Badge>
        )}
        {bypassActive && (
          <Badge variant="yolo">⚡ YOLO</Badge>
        )}
      </div>

      <Tooltip text={`Switch to ${theme === "dark" ? "light" : "dark"} theme`} position="bottom">
        <button
          onClick={toggleTheme}
          style={{
            color: "var(--color-text-dim)",
            fontSize: "var(--font-size-xs)",
            background: "none",
            border: "1px solid var(--color-border)",
            borderRadius: "var(--radius-sm)",
            padding: "3px 10px",
            cursor: "pointer",
            fontFamily: "var(--font-mono)",
            transition: "color var(--transition-fast), border-color var(--transition-fast)",
          }}
        >
          {theme === "dark" ? "☀ light" : "◑ dark"}
        </button>
      </Tooltip>
    </header>
  );
}
