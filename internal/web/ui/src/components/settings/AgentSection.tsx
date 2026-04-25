import type { AgentConfigProto } from "../../gen/feino/v1/feino_pb";

type Props = {
  agent: AgentConfigProto | undefined;
  onChange: (update: Partial<AgentConfigProto>) => void;
}

function NumericField({ label, value, onChange, min = 0 }: {
  label: string; value: number; onChange: (n: number) => void; min?: number;
}) {
  return (
    <label style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
      <span style={{ color: "var(--color-text-dim)", fontSize: "var(--font-size-sm)" }}>{label}</span>
      <input
        type="number"
        min={min}
        value={value}
        onChange={(e) => onChange(parseInt(e.target.value) || 0)}
        style={{
          background: "var(--color-surface-2)",
          color: "var(--color-text)",
          border: "1px solid var(--color-border)",
          borderRadius: "var(--radius-sm)",
          padding: "7px 10px",
          fontFamily: "var(--font-mono)",
          fontSize: "var(--font-size-sm)",
          outline: "none",
          width: "160px",
        }}
      />
    </label>
  );
}

export function AgentSection({ agent, onChange }: Props) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "16px", maxWidth: "520px" }}>
      <NumericField
        label="Max Retries"
        value={agent?.maxRetries ?? 5}
        onChange={(n) => onChange({ maxRetries: n })}
        min={1}
      />
      <NumericField
        label="High Complexity Threshold (tokens)"
        value={agent?.highComplexityThreshold ?? 2000}
        onChange={(n) => onChange({ highComplexityThreshold: n })}
      />
      <NumericField
        label="Low Complexity Threshold (tokens)"
        value={agent?.lowComplexityThreshold ?? 500}
        onChange={(n) => onChange({ lowComplexityThreshold: n })}
      />
      <div style={{ color: "var(--color-text-faint)", fontSize: "var(--font-size-xs)" }}>
        Inputs below the low threshold use lighter models; above the high threshold use more capable models.
      </div>
    </div>
  );
}
