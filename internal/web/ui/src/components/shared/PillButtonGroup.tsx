type PillOption = {
  value: string;
  label: string;
}

type PillButtonGroupProps = {
  options: PillOption[];
  value: string;
  onChange: (v: string) => void;
}

export function PillButtonGroup({ options, value, onChange }: PillButtonGroupProps) {
  return (
    <div style={{ display: "flex", gap: "8px", flexWrap: "wrap" }}>
      {options.map((opt) => (
        <button
          key={opt.value}
          onClick={() => onChange(opt.value)}
          aria-pressed={value === opt.value}
          style={{
            padding: "5px 14px",
            borderRadius: "var(--radius-pill)",
            border: "1px solid",
            cursor: "pointer",
            fontFamily: "var(--font-sans)",
            fontSize: "var(--font-size-sm)",
            transition: "all var(--transition-fast)",
            background: value === opt.value ? "var(--color-primary-muted)" : "transparent",
            borderColor: value === opt.value ? "var(--color-primary)" : "var(--color-border)",
            color: value === opt.value ? "var(--color-primary)" : "var(--color-text-dim)",
          }}
        >
          {opt.label}
        </button>
      ))}
    </div>
  );
}
