import type { EmailServiceConfigProto } from "../../gen/feino/v1/feino_pb";
import { TextInput } from "../shared/TextInput";

interface Props {
  email: EmailServiceConfigProto | undefined;
  onChange: (update: Partial<EmailServiceConfigProto>) => void;
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
      <span style={{ color: "var(--color-text-dim)", fontSize: "var(--font-size-sm)" }}>{label}</span>
      {children}
    </label>
  );
}

export function EmailSetupSection({ email: e, onChange }: Props) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "16px", maxWidth: "520px" }}>
      <label style={{ display: "flex", alignItems: "center", gap: "10px", cursor: "pointer" }}>
        <input
          type="checkbox"
          checked={e?.enabled ?? false}
          onChange={(ev) => onChange({ enabled: ev.target.checked })}
          style={{ accentColor: "var(--color-primary)", width: "16px", height: "16px" }}
        />
        <span style={{ color: "var(--color-text)", fontSize: "var(--font-size-sm)" }}>Enable email integration</span>
      </label>

      {e?.enabled && (
        <>
          <Field label="Email Address">
            <TextInput value={e?.address ?? ""} onChange={(v) => onChange({ address: v })} placeholder="you@example.com" />
          </Field>

          <div style={{ color: "var(--color-accent)", fontSize: "var(--font-size-xs)", fontFamily: "var(--font-mono)", marginTop: "4px" }}>
            IMAP (incoming)
          </div>

          <Field label="IMAP Host">
            <TextInput value={e?.imapHost ?? ""} onChange={(v) => onChange({ imapHost: v })} placeholder="imap.example.com" />
          </Field>
          <Field label="IMAP Port">
            <TextInput type="number" value={e?.imapPort ?? 993} onChange={(v) => onChange({ imapPort: parseInt(v) || 993 })} />
          </Field>

          <div style={{ color: "var(--color-accent)", fontSize: "var(--font-size-xs)", fontFamily: "var(--font-mono)", marginTop: "4px" }}>
            SMTP (outgoing)
          </div>

          <Field label="SMTP Host">
            <TextInput value={e?.smtpHost ?? ""} onChange={(v) => onChange({ smtpHost: v })} placeholder="smtp.example.com" />
          </Field>
          <Field label="SMTP Port">
            <TextInput type="number" value={e?.smtpPort ?? 587} onChange={(v) => onChange({ smtpPort: parseInt(v) || 587 })} />
          </Field>

          <Field label={`Password ${e?.hasPassword ? "(stored)" : "(not set)"}`}>
            <input
              type="password"
              placeholder="Enter new password to change"
              onChange={(ev) => onChange({ password: ev.target.value } as Partial<EmailServiceConfigProto>)}
              style={{
                background: "var(--color-surface-2)",
                color: "var(--color-text)",
                border: "1px solid var(--color-border)",
                borderRadius: "var(--radius-sm)",
                padding: "7px 10px",
                fontFamily: "var(--font-sans)",
                fontSize: "var(--font-size-sm)",
                outline: "none",
              }}
            />
          </Field>
        </>
      )}
    </div>
  );
}
