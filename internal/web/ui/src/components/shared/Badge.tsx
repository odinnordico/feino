import type { ReactNode } from "react";

type BadgeVariant = "primary" | "accent" | "error" | "warning" | "dim" | "thinking" | "tool" | "yolo";

type BadgeProps = {
  variant?: BadgeVariant;
  children: ReactNode;
  pulse?: boolean;
}

const variantStyles: Record<BadgeVariant, React.CSSProperties> = {
  primary:  { background: "var(--color-primary-muted)",   color: "var(--color-primary)",  border: "1px solid var(--color-primary)" },
  accent:   { background: "var(--color-tool-muted)",      color: "var(--color-accent)",   border: "1px solid var(--color-accent)" },
  error:    { background: "var(--color-error-muted)",     color: "var(--color-error)",    border: "1px solid var(--color-error)" },
  warning:  { background: "var(--color-warning-muted)",   color: "var(--color-warning)",  border: "1px solid var(--color-warning)" },
  dim:      { background: "var(--color-surface-2)",       color: "var(--color-text-dim)", border: "1px solid var(--color-border)" },
  thinking: { background: "var(--color-thinking-muted)",  color: "var(--color-thinking)", border: "1px solid var(--color-thinking)" },
  tool:     { background: "var(--color-tool-muted)",      color: "var(--color-tool)",     border: "1px solid var(--color-tool)" },
  yolo:     { background: "var(--color-yolo-muted)",      color: "var(--color-yolo)",     border: "1px solid var(--color-yolo)" },
};

export function Badge({ variant = "dim", children, pulse = false }: BadgeProps) {
  return (
    <span
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: "4px",
        padding: "1px 7px",
        borderRadius: "var(--radius-pill)",
        fontFamily: "var(--font-mono)",
        fontSize: "var(--font-size-xs)",
        fontWeight: 500,
        whiteSpace: "nowrap",
        animation: pulse ? "pulse-dot 1.4s ease-in-out infinite" : undefined,
        ...variantStyles[variant],
      }}
    >
      {children}
    </span>
  );
}
