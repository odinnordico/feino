import type { FileEntry } from "../../gen/feino/v1/feino_pb";
import { formatBytes } from "../../lib/utils";

type AtPathMenuProps = {
  entries: FileEntry[];
  onSelect: (path: string) => void;
  selectedIdx: number;
}

export function AtPathMenu({ entries, onSelect, selectedIdx }: AtPathMenuProps) {
  if (entries.length === 0) {return null;}

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
        maxHeight: "200px",
        overflowY: "auto",
      }}
    >
      {entries.map((entry, i) => (
        <button
          key={entry.relPath}
          onMouseDown={(e) => { e.preventDefault(); onSelect(entry.relPath); }}
          style={{
            display: "flex",
            alignItems: "center",
            gap: "10px",
            width: "100%",
            padding: "7px 14px",
            background: i === selectedIdx ? "var(--color-primary-muted)" : "transparent",
            border: "none",
            cursor: "pointer",
            textAlign: "left",
          }}
        >
          <span style={{ color: "var(--color-text-dim)", fontFamily: "var(--font-mono)", fontSize: "0.9rem" }}>
            {entry.isDir ? "📁" : "📄"}
          </span>
          <span style={{ fontFamily: "var(--font-mono)", fontSize: "var(--font-size-sm)", color: i === selectedIdx ? "var(--color-primary)" : "var(--color-text)" }}>
            {entry.name}
          </span>
          {!entry.isDir && (
            <span style={{ marginLeft: "auto", color: "var(--color-text-faint)", fontSize: "var(--font-size-xs)", fontFamily: "var(--font-mono)" }}>
              {formatBytes(Number(entry.size))}
            </span>
          )}
        </button>
      ))}
    </div>
  );
}
