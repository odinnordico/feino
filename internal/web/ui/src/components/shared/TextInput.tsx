type TextInputProps = {
  value: string | number;
  onChange: (v: string) => void;
  placeholder?: string;
  type?: string;
}

export function TextInput({ value, onChange, placeholder, type = "text" }: TextInputProps) {
  return (
    <input
      type={type}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
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
  );
}
