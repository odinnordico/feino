import { create } from "@bufbuild/protobuf";
import {
  type ProvidersConfigProto,
  AnthropicConfigProtoSchema,
  OpenAIConfigProtoSchema,
  GeminiConfigProtoSchema,
  OllamaConfigProtoSchema,
  OpenAICompatConfigProtoSchema,
} from "../../gen/feino/v1/feino_pb";
import { TextInput } from "../shared/TextInput";

function proto<T extends object>(msg: T | undefined): Omit<T, "$typeName" | "$unknown"> {
  const { $typeName: _t, $unknown: _u, ...rest } = (msg ?? {}) as T & { $typeName?: unknown; $unknown?: unknown };
  return rest as Omit<T, "$typeName" | "$unknown">;
}

type Props = {
  providers: ProvidersConfigProto | undefined;
  onChange: (update: Partial<ProvidersConfigProto>) => void;
}

function FieldRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
      <span style={{ color: "var(--color-text-dim)", fontSize: "var(--font-size-sm)" }}>{label}</span>
      {children}
    </label>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <div style={{ color: "var(--color-primary)", fontFamily: "var(--font-mono)", fontSize: "var(--font-size-sm)", marginBottom: "10px", fontWeight: 600 }}>
        {title}
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: "10px", paddingLeft: "8px", borderLeft: "2px solid var(--color-border)" }}>
        {children}
      </div>
    </div>
  );
}

export function ProviderSection({ providers: p, onChange }: Props) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "20px", maxWidth: "520px" }}>
      <Section title="Anthropic">
        <FieldRow label={`API Key ${p?.anthropic?.hasApiKey ? "(stored)" : "(not set)"}`}>
          <TextInput type="password" value={p?.anthropic?.apiKey ?? ""} onChange={(v) => onChange({ anthropic: create(AnthropicConfigProtoSchema, { ...proto(p?.anthropic), apiKey: v }) })} placeholder="sk-ant-…" />
        </FieldRow>
        <FieldRow label="Default Model">
          <TextInput value={p?.anthropic?.defaultModel ?? ""} onChange={(v) => onChange({ anthropic: create(AnthropicConfigProtoSchema, { ...proto(p?.anthropic), defaultModel: v }) })} placeholder="claude-opus-4-6" />
        </FieldRow>
      </Section>

      <Section title="OpenAI">
        <FieldRow label={`API Key ${p?.openai?.hasApiKey ? "(stored)" : "(not set)"}`}>
          <TextInput type="password" value={p?.openai?.apiKey ?? ""} onChange={(v) => onChange({ openai: create(OpenAIConfigProtoSchema, { ...proto(p?.openai), apiKey: v }) })} placeholder="sk-…" />
        </FieldRow>
        <FieldRow label="Base URL (optional)">
          <TextInput value={p?.openai?.baseUrl ?? ""} onChange={(v) => onChange({ openai: create(OpenAIConfigProtoSchema, { ...proto(p?.openai), baseUrl: v }) })} placeholder="https://api.openai.com/v1" />
        </FieldRow>
        <FieldRow label="Default Model">
          <TextInput value={p?.openai?.defaultModel ?? ""} onChange={(v) => onChange({ openai: create(OpenAIConfigProtoSchema, { ...proto(p?.openai), defaultModel: v }) })} />
        </FieldRow>
      </Section>

      <Section title="Gemini">
        <FieldRow label={`API Key ${p?.gemini?.hasApiKey ? "(stored)" : "(not set)"}`}>
          <TextInput type="password" value={p?.gemini?.apiKey ?? ""} onChange={(v) => onChange({ gemini: create(GeminiConfigProtoSchema, { ...proto(p?.gemini), apiKey: v }) })} placeholder="AIza…" />
        </FieldRow>
        <FieldRow label="Default Model">
          <TextInput value={p?.gemini?.defaultModel ?? ""} onChange={(v) => onChange({ gemini: create(GeminiConfigProtoSchema, { ...proto(p?.gemini), defaultModel: v }) })} placeholder="gemini-2.0-flash" />
        </FieldRow>
      </Section>

      <Section title="Ollama">
        <FieldRow label="Host">
          <TextInput value={p?.ollama?.host ?? ""} onChange={(v) => onChange({ ollama: create(OllamaConfigProtoSchema, { ...proto(p?.ollama), host: v }) })} placeholder="http://localhost:11434" />
        </FieldRow>
        <FieldRow label="Default Model">
          <TextInput value={p?.ollama?.defaultModel ?? ""} onChange={(v) => onChange({ ollama: create(OllamaConfigProtoSchema, { ...proto(p?.ollama), defaultModel: v }) })} />
        </FieldRow>
      </Section>

      <Section title="OpenAI-Compatible">
        <FieldRow label="Base URL">
          <TextInput value={p?.openaiCompat?.baseUrl ?? ""} onChange={(v) => onChange({ openaiCompat: create(OpenAICompatConfigProtoSchema, { ...proto(p?.openaiCompat), baseUrl: v }) })} placeholder="http://…/v1" />
        </FieldRow>
        <FieldRow label="Display Name">
          <TextInput value={p?.openaiCompat?.name ?? ""} onChange={(v) => onChange({ openaiCompat: create(OpenAICompatConfigProtoSchema, { ...proto(p?.openaiCompat), name: v }) })} />
        </FieldRow>
        <FieldRow label="Default Model">
          <TextInput value={p?.openaiCompat?.defaultModel ?? ""} onChange={(v) => onChange({ openaiCompat: create(OpenAICompatConfigProtoSchema, { ...proto(p?.openaiCompat), defaultModel: v }) })} />
        </FieldRow>
        <FieldRow label={`API Key ${p?.openaiCompat?.hasApiKey ? "(stored)" : "(optional)"}`}>
          <TextInput type="password" value={p?.openaiCompat?.apiKey ?? ""} onChange={(v) => onChange({ openaiCompat: create(OpenAICompatConfigProtoSchema, { ...proto(p?.openaiCompat), apiKey: v }) })} />
        </FieldRow>
      </Section>
    </div>
  );
}
