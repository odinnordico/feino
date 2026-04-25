import { useState } from "react";
import type { SecurityConfigProto } from "../../gen/feino/v1/feino_pb";

const PERMISSION_LEVELS = ["read", "write", "bash", "danger_zone"] as const;

type Props = {
  security: SecurityConfigProto | undefined;
  onChange: (update: Partial<SecurityConfigProto>) => void;
}

export function SecuritySection({ security: sec, onChange }: Props) {
  const [newPath, setNewPath] = useState("");

  function addPath() {
    if (!newPath.trim()) {return;}
    const paths = [...(sec?.allowedPaths ?? []), newPath.trim()];
    onChange({ allowedPaths: paths });
    setNewPath("");
  }

  function removePath(idx: number) {
    const paths = (sec?.allowedPaths ?? []).filter((_, i) => i !== idx);
    onChange({ allowedPaths: paths });
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "20px", maxWidth: "520px" }}>
      <div>
        <div style={{ color: "var(--color-text-dim)", fontSize: "var(--font-size-sm)", marginBottom: "10px" }}>
          Permission Level
        </div>
        <div style={{ display: "flex", gap: "8px", flexWrap: "wrap" }}>
          {PERMISSION_LEVELS.map((lvl) => (
            <button
              key={lvl}
              onClick={() => onChange({ permissionLevel: lvl })}
              aria-pressed={sec?.permissionLevel === lvl}
              style={{
                padding: "5px 14px",
                borderRadius: "var(--radius-pill)",
                border: "1px solid",
                cursor: "pointer",
                fontFamily: "var(--font-mono)",
                fontSize: "var(--font-size-xs)",
                transition: "all var(--transition-fast)",
                background: sec?.permissionLevel === lvl ? "var(--color-primary-muted)" : "transparent",
                borderColor: sec?.permissionLevel === lvl ? "var(--color-primary)" : "var(--color-border)",
                color: sec?.permissionLevel === lvl ? "var(--color-primary)" : "var(--color-text-dim)",
              }}
            >
              {lvl}
            </button>
          ))}
        </div>
        <div style={{ color: "var(--color-text-faint)", fontSize: "var(--font-size-xs)", marginTop: "6px" }}>
          {levelDescriptions[sec?.permissionLevel ?? "read"]}
        </div>
      </div>

      <div>
        <div style={{ color: "var(--color-text-dim)", fontSize: "var(--font-size-sm)", marginBottom: "8px" }}>
          Allowed Paths
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: "4px", marginBottom: "8px" }}>
          {(sec?.allowedPaths ?? []).map((p, i) => (
            <div key={p} style={{ display: "flex", alignItems: "center", gap: "8px" }}>
              <code style={{ flex: 1, fontFamily: "var(--font-mono)", fontSize: "var(--font-size-xs)", color: "var(--color-text-dim)", background: "var(--color-surface-2)", padding: "3px 8px", borderRadius: "var(--radius-sm)" }}>
                {p}
              </code>
              <button onClick={() => removePath(i)} style={{ color: "var(--color-error)", background: "none", border: "none", cursor: "pointer", fontSize: "var(--font-size-sm)" }}>✕</button>
            </div>
          ))}
        </div>
        <div style={{ display: "flex", gap: "8px" }}>
          <input
            value={newPath}
            onChange={(e) => setNewPath(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && addPath()}
            placeholder="/home/user/project"
            style={{
              flex: 1,
              background: "var(--color-surface-2)",
              color: "var(--color-text)",
              border: "1px solid var(--color-border)",
              borderRadius: "var(--radius-sm)",
              padding: "6px 10px",
              fontFamily: "var(--font-mono)",
              fontSize: "var(--font-size-xs)",
              outline: "none",
            }}
          />
          <button onClick={addPath} style={{ color: "var(--color-primary)", background: "var(--color-primary-muted)", border: "1px solid var(--color-primary)", borderRadius: "var(--radius-sm)", padding: "4px 12px", cursor: "pointer", fontFamily: "var(--font-mono)", fontSize: "var(--font-size-xs)" }}>
            Add
          </button>
        </div>
      </div>

      <label style={{ display: "flex", alignItems: "center", gap: "10px", cursor: "pointer" }}>
        <input
          type="checkbox"
          checked={sec?.enableAstBlacklist ?? false}
          onChange={(e) => onChange({ enableAstBlacklist: e.target.checked })}
          style={{ accentColor: "var(--color-primary)", width: "16px", height: "16px" }}
        />
        <span style={{ color: "var(--color-text)", fontSize: "var(--font-size-sm)" }}>
          Enable AST blacklist (block dangerous patterns in generated code)
        </span>
      </label>
    </div>
  );
}

const levelDescriptions: Record<string, string> = {
  read:        "Agent can only read files and run safe queries.",
  write:       "Agent can read and write files.",
  bash:        "Agent can run arbitrary shell commands.",
  danger_zone: "No restrictions. Use only in sandboxed environments.",
};
