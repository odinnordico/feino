import { useState, useEffect } from "react";
import { create } from "@bufbuild/protobuf";
import { UserProfileConfigProtoSchema } from "../../gen/feino/v1/feino_pb";
import { useConfig } from "../../hooks/useConfig";
import type { CommStyle } from "../../types/config";
import { Button } from "../shared/Button";
import { Spinner } from "../shared/Spinner";
import { MemoryManager } from "./MemoryManager";

const COMM_STYLES = ["concise", "detailed", "technical", "friendly"] as const;

function proto<T extends object>(msg: T | undefined): Omit<T, "$typeName" | "$unknown"> {
  const { $typeName: _t, $unknown: _u, ...rest } = (msg ?? {}) as T & { $typeName?: unknown; $unknown?: unknown };
  return rest as Omit<T, "$typeName" | "$unknown">;
}

export function ProfilePage() {
  const { config, loadConfig, saveConfig } = useConfig();
  const [loading, setLoading] = useState(false);
  const [saved,   setSaved]   = useState(false);

  const [name,     setName]     = useState("");
  const [timezone, setTimezone] = useState("");
  const [style,    setStyle]    = useState<CommStyle>("concise");

  useEffect(() => {
    setLoading(true);
    loadConfig().then((cfg) => {
      if (cfg?.user) {
        setName(cfg.user.name ?? "");
        setTimezone(cfg.user.timezone ?? "");
        setStyle((cfg.user.communicationStyle as CommStyle) || "concise");
      }
    }).finally(() => setLoading(false));
  }, [loadConfig]);

  async function handleSave() {
    if (!config) return;
    await saveConfig({
      ...config,
      user: create(UserProfileConfigProtoSchema, { ...proto(config.user), name, timezone, communicationStyle: style }),
    });
    setSaved(true);
    setTimeout(() => setSaved(false), 2000);
  }

  if (loading) return <div style={{ padding: "24px" }}><Spinner /></div>;

  return (
    <div style={{ display: "flex", flexDirection: "column", height: "100%", overflowY: "auto", padding: "24px", gap: "32px" }}>
      {/* Profile section */}
      <section>
        <h1 style={{ color: "var(--color-primary)", fontFamily: "var(--font-mono)", fontSize: "var(--font-size-lg)", margin: "0 0 20px" }}>
          Profile
        </h1>

        <div style={{ display: "flex", flexDirection: "column", gap: "16px", maxWidth: "480px" }}>
          <label>
            <div style={{ color: "var(--color-text-dim)", fontSize: "var(--font-size-sm)", marginBottom: "4px" }}>Name</div>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Diego"
              style={inputStyle}
            />
          </label>

          <label>
            <div style={{ color: "var(--color-text-dim)", fontSize: "var(--font-size-sm)", marginBottom: "4px" }}>Timezone</div>
            <input
              value={timezone}
              onChange={(e) => setTimezone(e.target.value)}
              placeholder="e.g. America/New_York"
              style={inputStyle}
            />
          </label>

          <div>
            <div style={{ color: "var(--color-text-dim)", fontSize: "var(--font-size-sm)", marginBottom: "8px" }}>
              Communication style
            </div>
            <div style={{ display: "flex", gap: "8px", flexWrap: "wrap" }}>
              {COMM_STYLES.map((s) => (
                <button
                  key={s}
                  onClick={() => setStyle(s)}
                  aria-pressed={style === s}
                  style={{
                    padding: "5px 14px",
                    borderRadius: "var(--radius-pill)",
                    border: "1px solid",
                    cursor: "pointer",
                    fontFamily: "var(--font-sans)",
                    fontSize: "var(--font-size-sm)",
                    transition: "all var(--transition-fast)",
                    background: style === s ? "var(--color-primary-muted)" : "transparent",
                    borderColor: style === s ? "var(--color-primary)" : "var(--color-border)",
                    color: style === s ? "var(--color-primary)" : "var(--color-text-dim)",
                  }}
                >
                  {s}
                </button>
              ))}
            </div>
          </div>

          <div style={{ display: "flex", alignItems: "center", gap: "12px" }}>
            <Button variant="primary" onClick={handleSave}>Save Profile</Button>
            {saved && <span style={{ color: "var(--color-primary)", fontSize: "var(--font-size-sm)" }}>✓ Saved</span>}
          </div>
        </div>
      </section>

      {/* Memory manager */}
      <section>
        <h2 style={{ color: "var(--color-text)", fontFamily: "var(--font-mono)", fontSize: "var(--font-size-base)", margin: "0 0 16px" }}>
          Memories
        </h2>
        <MemoryManager />
      </section>
    </div>
  );
}

const inputStyle: React.CSSProperties = {
  width: "100%",
  background: "var(--color-surface-2)",
  color: "var(--color-text)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius-sm)",
  padding: "8px 12px",
  fontFamily: "var(--font-sans)",
  fontSize: "var(--font-size-sm)",
  outline: "none",
};
