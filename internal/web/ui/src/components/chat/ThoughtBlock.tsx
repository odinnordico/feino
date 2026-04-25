import { Collapsible } from "../shared/Collapsible";

type ThoughtBlockProps = {
  text: string;
  isStreaming?: boolean;
}

export function ThoughtBlock({ text, isStreaming = false }: ThoughtBlockProps) {
  const label = isStreaming ? "💭 Thinking…" : "💭 Thought";

  return (
    <Collapsible
      header={<span>{label}</span>}
      defaultOpen={false}
      accentColor="var(--color-thinking)"
    >
      <pre
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: "var(--font-size-sm)",
          color: "var(--color-text-dim)",
          background: "var(--color-thinking-muted)",
          border: "1px solid var(--color-thinking)",
          borderRadius: "var(--radius-sm)",
          padding: "8px 12px",
          whiteSpace: "pre-wrap",
          wordBreak: "break-word",
          margin: 0,
          lineHeight: 1.6,
        }}
      >
        {text}
      </pre>
    </Collapsible>
  );
}
