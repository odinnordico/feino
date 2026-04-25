import { useState, useEffect, type ReactNode } from "react";

type CollapsibleProps = {
  header: ReactNode;
  children: ReactNode;
  defaultOpen?: boolean;
  accentColor?: string;
}

export function Collapsible({
  header,
  children,
  defaultOpen = false,
  accentColor = "var(--color-primary)",
}: CollapsibleProps) {
  const [open, setOpen] = useState(defaultOpen);

  useEffect(() => {
    setOpen(defaultOpen ?? false);
  }, [defaultOpen]);

  return (
    <div
      style={{
        borderLeft: `2px solid ${accentColor}`,
        paddingLeft: "8px",
        margin: "4px 0",
      }}
    >
      <button
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        style={{
          display: "flex",
          alignItems: "center",
          gap: "6px",
          background: "none",
          border: "none",
          cursor: "pointer",
          color: accentColor,
          fontFamily: "var(--font-mono)",
          fontSize: "var(--font-size-xs)",
          padding: "2px 0",
          width: "100%",
          textAlign: "left",
          transition: "opacity var(--transition-fast)",
        }}
      >
        <span style={{ transition: "transform var(--transition-fast)", transform: open ? "rotate(90deg)" : "rotate(0deg)" }}>
          ▶
        </span>
        {header}
      </button>

      {open && (
        <div style={{ marginTop: "4px", animation: "fadeSlideUp var(--transition-fast) ease both" }}>
          {children}
        </div>
      )}
    </div>
  );
}
