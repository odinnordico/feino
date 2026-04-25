import { create } from "@bufbuild/protobuf";
import { feinoClient } from "../../client";
import { SetLanguageRequestSchema } from "../../gen/feino/v1/feino_pb";
import { useSessionStore } from "../../store/sessionStore";
import { Modal } from "../shared/Modal";
import { Button } from "../shared/Button";

const LANGUAGES = [
  { code: "en",    label: "English" },
  { code: "es-419",label: "Español (Latin America)" },
  { code: "es",    label: "Español (España)" },
  { code: "pt-BR", label: "Português (Brasil)" },
  { code: "pt",    label: "Português (Portugal)" },
  { code: "zh",    label: "中文 (简体)" },
  { code: "ja",    label: "日本語" },
  { code: "ru",    label: "Русский" },
];

type LangModalProps = { onClose: () => void; }

export function LangModal({ onClose }: LangModalProps) {
  const { language, setLanguage } = useSessionStore();

  async function select(code: string) {
    setLanguage(code);
    try { await feinoClient.setLanguage(create(SetLanguageRequestSchema, { code })); } catch { /* best effort */ }
    onClose();
  }

  return (
    <Modal title="🌐 Select Language" onClose={onClose}>
      <div style={{ display: "flex", flexDirection: "column", gap: "6px", marginBottom: "16px" }}>
        {LANGUAGES.map((l) => (
          <button
            key={l.code}
            onClick={() => select(l.code)}
            aria-pressed={language === l.code}
            style={{
              display: "flex",
              alignItems: "center",
              gap: "10px",
              padding: "8px 14px",
              borderRadius: "var(--radius-sm)",
              border: "1px solid",
              cursor: "pointer",
              background: language === l.code ? "var(--color-primary-muted)" : "var(--color-surface-2)",
              borderColor: language === l.code ? "var(--color-primary)" : "var(--color-border)",
              color: language === l.code ? "var(--color-primary)" : "var(--color-text)",
              fontFamily: "var(--font-sans)",
              fontSize: "var(--font-size-sm)",
              textAlign: "left",
            }}
          >
            {language === l.code && <span>●</span>}
            {language !== l.code && <span style={{ color: "var(--color-text-faint)" }}>○</span>}
            {l.label}
          </button>
        ))}
      </div>
      <Button variant="ghost" onClick={onClose}>Cancel</Button>
    </Modal>
  );
}
