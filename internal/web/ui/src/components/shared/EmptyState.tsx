type EmptyStateProps = {
  icon?: string;
  title: string;
  description?: string;
  action?: { label: string; onClick: () => void };
}

export function EmptyState({ icon = "◈", title, description, action }: EmptyStateProps) {
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        gap: "12px",
        padding: "48px 24px",
        textAlign: "center",
      }}
    >
      <div
        style={{
          fontSize: "2.5rem",
          color: "var(--color-text-faint)",
          fontFamily: "var(--font-mono)",
          lineHeight: 1,
        }}
      >
        {icon}
      </div>
      <div
        style={{
          color: "var(--color-text-dim)",
          fontFamily: "var(--font-mono)",
          fontSize: "var(--font-size-base)",
        }}
      >
        {title}
      </div>
      {description && (
        <div
          style={{
            color: "var(--color-text-faint)",
            fontFamily: "var(--font-sans)",
            fontSize: "var(--font-size-sm)",
            maxWidth: "320px",
            lineHeight: 1.6,
          }}
        >
          {description}
        </div>
      )}
      {action && (
        <button
          onClick={action.onClick}
          style={{
            marginTop: "8px",
            padding: "8px 20px",
            background: "var(--color-primary-muted)",
            border: "1px solid var(--color-primary)",
            borderRadius: "var(--radius-md)",
            color: "var(--color-primary)",
            fontFamily: "var(--font-mono)",
            fontSize: "var(--font-size-sm)",
            cursor: "pointer",
          }}
        >
          {action.label}
        </button>
      )}
    </div>
  );
}
