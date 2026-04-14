import type { ButtonHTMLAttributes } from "react";

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "primary" | "ghost" | "danger";
}

export function Button({ variant = "primary", style, disabled, ...props }: ButtonProps) {
  const base: React.CSSProperties = {
    fontFamily: "var(--font-mono)",
    fontSize: "0.85rem",
    padding: "6px 14px",
    borderRadius: "var(--radius-sm)",
    cursor: disabled ? "not-allowed" : "pointer",
    transition: "opacity var(--transition-fast), background var(--transition-fast)",
    opacity: disabled ? 0.5 : 1,
    ...style,
  };

  const variants: Record<string, React.CSSProperties> = {
    primary: {
      background: "var(--color-primary)",
      color: "var(--color-bg)",
      border: "none",
      fontWeight: 700,
    },
    ghost: {
      background: "transparent",
      color: "var(--color-primary)",
      border: "1px solid var(--color-border)",
    },
    danger: {
      background: "transparent",
      color: "var(--color-error)",
      border: "1px solid var(--color-error)",
    },
  };

  return <button style={{ ...base, ...variants[variant] }} disabled={disabled} {...props} />;
}
