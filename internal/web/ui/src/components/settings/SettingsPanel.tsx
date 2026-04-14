import { useState, useEffect, useCallback } from "react";
import { create } from "@bufbuild/protobuf";
import { useConfig } from "../../hooks/useConfig";
import { Button } from "../shared/Button";
import { SkeletonLines } from "../shared/Skeleton";
import { ProviderSection } from "./ProviderSection";
import { SecuritySection } from "./SecuritySection";
import { AgentSection } from "./AgentSection";
import { EmailSetupSection } from "./EmailSetupSection";
import type { ConfigProto } from "../../gen/feino/v1/feino_pb";

function proto<T extends object>(msg: T | undefined): Omit<T, "$typeName" | "$unknown"> {
  const { $typeName: _t, $unknown: _u, ...rest } = (msg ?? {}) as T & { $typeName?: unknown; $unknown?: unknown };
  return rest as Omit<T, "$typeName" | "$unknown">;
}
import {
  ProvidersConfigProtoSchema,
  SecurityConfigProtoSchema,
  AgentConfigProtoSchema,
  ContextConfigProtoSchema,
  ServicesConfigProtoSchema,
  EmailServiceConfigProtoSchema,
} from "../../gen/feino/v1/feino_pb";

type Tab = "providers" | "security" | "agent" | "context" | "email" | "advanced";

const TABS: { id: Tab; label: string }[] = [
  { id: "providers", label: "Providers" },
  { id: "security",  label: "Security"  },
  { id: "agent",     label: "Agent"     },
  { id: "context",   label: "Context"   },
  { id: "email",     label: "Email"     },
  { id: "advanced",  label: "Advanced"  },
];

export function SettingsPanel() {
  const { loadConfig, saveConfig, getYAML } = useConfig();
  const [loading,  setLoading]  = useState(false);
  const [tab,      setTab]      = useState<Tab>("providers");
  const [draft,    setDraft]    = useState<ConfigProto | null>(null);
  const [yaml,     setYaml]     = useState("");
  const [saving,   setSaving]   = useState(false);
  const [savedMsg, setSavedMsg] = useState("");

  useEffect(() => {
    setLoading(true);
    loadConfig().then((cfg) => { if (cfg) setDraft(cfg); }).finally(() => setLoading(false));
  }, [loadConfig]);

  async function handleSave() {
    if (!draft) return;
    setSaving(true);
    try {
      await saveConfig(draft);
      setSavedMsg("✓ Saved");
      setTimeout(() => setSavedMsg(""), 2500);
    } catch (err) {
      setSavedMsg(`Error: ${err instanceof Error ? err.message : "Unknown error"}`);
    } finally {
      setSaving(false);
    }
  }

  async function loadYaml() {
    const y = await getYAML();
    setYaml(y);
  }

  const setField = useCallback(<K extends keyof ConfigProto>(key: K, val: ConfigProto[K]) => {
    setDraft((d) => d ? { ...d, [key]: val } : d);
  }, []);

  if (loading || !draft) return (
    <div style={{ padding: "24px", display: "flex", flexDirection: "column", gap: "16px", maxWidth: "520px" }}>
      <SkeletonLines count={5} lastWidth="40%" />
    </div>
  );

  return (
    <div style={{ display: "flex", flexDirection: "column", height: "100%", padding: "24px" }}>
      {/* Header */}
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: "20px" }}>
        <h1 style={{ color: "var(--color-primary)", fontFamily: "var(--font-mono)", fontSize: "var(--font-size-lg)", margin: 0 }}>
          Settings
        </h1>
        <div style={{ display: "flex", gap: "10px", alignItems: "center" }}>
          {savedMsg && (
            <span style={{ color: savedMsg.startsWith("✓") ? "var(--color-primary)" : "var(--color-error)", fontSize: "var(--font-size-sm)" }}>
              {savedMsg}
            </span>
          )}
          <Button variant="primary" onClick={handleSave} disabled={saving}>
            {saving ? "Saving…" : "Save"}
          </Button>
        </div>
      </div>

      {/* Tab bar */}
      <div style={{ display: "flex", gap: "2px", borderBottom: "1px solid var(--color-border)", marginBottom: "20px", flexWrap: "wrap" }}>
        {TABS.map((t) => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            style={{
              padding: "7px 16px",
              border: "none",
              borderBottom: tab === t.id ? "2px solid var(--color-primary)" : "2px solid transparent",
              background: "transparent",
              cursor: "pointer",
              fontFamily: "var(--font-sans)",
              fontSize: "var(--font-size-sm)",
              color: tab === t.id ? "var(--color-primary)" : "var(--color-text-dim)",
              transition: "color var(--transition-fast)",
            }}
          >
            {t.label}
          </button>
        ))}
      </div>

      {/* Tab content */}
      <div style={{ flex: 1, overflowY: "auto" }}>
        {tab === "providers" && (
          <ProviderSection
            providers={draft.providers}
            onChange={(update) => setField("providers", create(ProvidersConfigProtoSchema, { ...proto(draft.providers), ...update }))}
          />
        )}

        {tab === "security" && (
          <SecuritySection
            security={draft.security}
            onChange={(update) => setField("security", create(SecurityConfigProtoSchema, { ...proto(draft.security), ...update }))}
          />
        )}

        {tab === "agent" && (
          <AgentSection
            agent={draft.agent}
            onChange={(update) => setField("agent", create(AgentConfigProtoSchema, { ...proto(draft.agent), ...update }))}
          />
        )}

        {tab === "context" && (
          <div style={{ display: "flex", flexDirection: "column", gap: "16px", maxWidth: "520px" }}>
            {[
              { label: "Working Directory",  key: "workingDir" as const,        placeholder: "/home/user/project" },
              { label: "Global Config Path", key: "globalConfigPath" as const,  placeholder: "~/.feino/config.md" },
              { label: "Plugins Directory",  key: "pluginsDir" as const,        placeholder: "~/.feino/plugins" },
            ].map(({ label, key, placeholder }) => (
              <label key={key} style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
                <span style={{ color: "var(--color-text-dim)", fontSize: "var(--font-size-sm)" }}>{label}</span>
                <input
                  value={draft.context?.[key] ?? ""}
                  onChange={(e) => setField("context", create(ContextConfigProtoSchema, { ...proto(draft.context), [key]: e.target.value }))}
                  placeholder={placeholder}
                  style={{ background: "var(--color-surface-2)", color: "var(--color-text)", border: "1px solid var(--color-border)", borderRadius: "var(--radius-sm)", padding: "7px 10px", fontFamily: "var(--font-sans)", fontSize: "var(--font-size-sm)", outline: "none" }}
                />
              </label>
            ))}
            <label style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
              <span style={{ color: "var(--color-text-dim)", fontSize: "var(--font-size-sm)" }}>Max Budget (chars)</span>
              <input
                type="number"
                value={draft.context?.maxBudget ?? 0}
                onChange={(e) => setField("context", create(ContextConfigProtoSchema, { ...proto(draft.context), maxBudget: parseInt(e.target.value) || 0 }))}
                style={{ background: "var(--color-surface-2)", color: "var(--color-text)", border: "1px solid var(--color-border)", borderRadius: "var(--radius-sm)", padding: "7px 10px", fontFamily: "var(--font-mono)", fontSize: "var(--font-size-sm)", outline: "none", width: "160px" }}
              />
            </label>
          </div>
        )}

        {tab === "email" && (
          <EmailSetupSection
            email={draft.services?.email}
            onChange={(update) => setField("services", create(ServicesConfigProtoSchema, {
              ...proto(draft.services),
              email: create(EmailServiceConfigProtoSchema, { ...proto(draft.services?.email), ...update }),
            }))}
          />
        )}

        {tab === "advanced" && (
          <div style={{ display: "flex", flexDirection: "column", gap: "16px" }}>
            <Button variant="ghost" onClick={loadYaml}>Load Config YAML</Button>
            {yaml && (
              <pre style={{
                background: "var(--color-surface-2)",
                border: "1px solid var(--color-border)",
                borderRadius: "var(--radius-md)",
                padding: "16px",
                fontFamily: "var(--font-mono)",
                fontSize: "var(--font-size-xs)",
                color: "var(--color-text)",
                overflow: "auto",
                maxHeight: "400px",
                whiteSpace: "pre-wrap",
                wordBreak: "break-word",
              }}>
                {yaml}
              </pre>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
