import { useState, type ReactNode } from "react";

type TooltipProps = {
  text: string;
  children: ReactNode;
  position?: "top" | "bottom" | "left" | "right";
}

export function Tooltip({ text, children, position = "top" }: TooltipProps) {
  const [visible, setVisible] = useState(false);

  const offset: React.CSSProperties = {
    top:    { bottom: "calc(100% + 6px)", left: "50%", transform: "translateX(-50%)" },
    bottom: { top:    "calc(100% + 6px)", left: "50%", transform: "translateX(-50%)" },
    left:   { right:  "calc(100% + 6px)", top:  "50%", transform: "translateY(-50%)" },
    right:  { left:   "calc(100% + 6px)", top:  "50%", transform: "translateY(-50%)" },
  }[position];

  return (
    <span
      style={{ position: "relative", display: "inline-flex" }}
      onMouseEnter={() => setVisible(true)}
      onMouseLeave={() => setVisible(false)}
      onFocus={() => setVisible(true)}
      onBlur={() => setVisible(false)}
    >
      {children}
      {visible && (
        <span
          style={{
            position: "absolute",
            ...offset,
            background: "var(--color-surface-3)",
            color: "var(--color-text)",
            border: "1px solid var(--color-border)",
            borderRadius: "var(--radius-sm)",
            padding: "4px 8px",
            fontSize: "var(--font-size-xs)",
            fontFamily: "var(--font-sans)",
            whiteSpace: "nowrap",
            zIndex: 200,
            pointerEvents: "none",
            boxShadow: "var(--shadow-md)",
          }}
        >
          {text}
        </span>
      )}
    </span>
  );
}
