interface SlashCommand {
  command: string;
  description: string;
}

const COMMANDS: SlashCommand[] = [
  { command: "/clear",          description: "Clear the screen" },
  { command: "/config",         description: "Show active config as YAML" },
  { command: "/history",        description: "Show message history" },
  { command: "/lang",           description: "Change UI language" },
  { command: "/profile",        description: "Open user profile" },
  { command: "/reload-plugins", description: "Reload tool plugins" },
  { command: "/reset",          description: "Clear conversation" },
  { command: "/settings",       description: "Open settings" },
  { command: "/theme",          description: "Change theme" },
  { command: "/yolo",           description: "Enable bypass mode" },
];

interface SlashCommandMenuProps {
  query: string;        // text after "/"
  onSelect: (command: string) => void;
  selectedIdx: number;
}

export function SlashCommandMenu({ query, onSelect, selectedIdx }: SlashCommandMenuProps) {
  const filtered = COMMANDS.filter((c) =>
    c.command.toLowerCase().includes(query.toLowerCase())
  );

  if (filtered.length === 0) return null;

  return (
    <div
      style={{
        position: "absolute",
        bottom: "100%",
        left: 0,
        right: 0,
        background: "var(--color-surface-2)",
        border: "1px solid var(--color-border)",
        borderRadius: "var(--radius-md)",
        marginBottom: "4px",
        overflow: "hidden",
        boxShadow: "var(--shadow-md)",
        zIndex: 50,
      }}
    >
      {filtered.map((cmd, i) => (
        <button
          key={cmd.command}
          onMouseDown={(e) => { e.preventDefault(); onSelect(cmd.command); }}
          style={{
            display: "flex",
            alignItems: "center",
            gap: "12px",
            width: "100%",
            padding: "8px 14px",
            background: i === selectedIdx ? "var(--color-primary-muted)" : "transparent",
            border: "none",
            cursor: "pointer",
            textAlign: "left",
          }}
        >
          <span style={{ fontFamily: "var(--font-mono)", fontSize: "var(--font-size-sm)", color: "var(--color-primary)", minWidth: "140px" }}>
            {cmd.command}
          </span>
          <span style={{ fontSize: "var(--font-size-xs)", color: "var(--color-text-dim)" }}>
            {cmd.description}
          </span>
        </button>
      ))}
    </div>
  );
}

export { COMMANDS };
