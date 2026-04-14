import { create } from "@bufbuild/protobuf";
import { feinoClient } from "../../client";
import { SetThemeRequestSchema } from "../../gen/feino/v1/feino_pb";
import { useSessionStore } from "../../store/sessionStore";
import type { Theme } from "../../types/config";
import { Modal } from "../shared/Modal";
import { Button } from "../shared/Button";

const THEMES: { value: Theme; label: string; desc: string }[] = [
  { value: "dark",  label: "Dark",  desc: "Catppuccin Mocha" },
  { value: "light", label: "Light", desc: "Catppuccin Latte" },
  { value: "neo",   label: "Neo",   desc: "Phosphor green" },
  { value: "auto",  label: "Auto",  desc: "Detect background" },
];

interface ThemeModalProps { onClose: () => void; }

export function ThemeModal({ onClose }: ThemeModalProps) {
  const { theme, setTheme } = useSessionStore();

  async function select(t: Theme) {
    setTheme(t);
    try { await feinoClient.setTheme(create(SetThemeRequestSchema, { theme: t })); } catch { /* best effort */ }
    onClose();
  }

  return (
    <Modal title="🎨 Select Theme" onClose={onClose}>
      <div style={{ display: "flex", flexDirection: "column", gap: "8px", marginBottom: "16px" }}>
        {THEMES.map((t) => (
          <button
            key={t.value}
            onClick={() => select(t.value)}
            aria-pressed={theme === t.value}
            style={{
              display: "flex",
              alignItems: "center",
              gap: "12px",
              padding: "10px 14px",
              borderRadius: "var(--radius-md)",
              border: "1px solid",
              cursor: "pointer",
              background: theme === t.value ? "var(--color-primary-muted)" : "var(--color-surface-2)",
              borderColor: theme === t.value ? "var(--color-primary)" : "var(--color-border)",
              color: theme === t.value ? "var(--color-primary)" : "var(--color-text)",
              fontFamily: "var(--font-sans)",
              fontSize: "var(--font-size-sm)",
              textAlign: "left",
            }}
          >
            <span style={{ width: "80px", fontWeight: 600 }}>{t.label}</span>
            <span style={{ color: "var(--color-text-dim)", fontSize: "var(--font-size-xs)" }}>{t.desc}</span>
          </button>
        ))}
      </div>
      <Button variant="ghost" onClick={onClose}>Cancel</Button>
    </Modal>
  );
}
