import { Collapsible } from "../shared/Collapsible";
import { Badge } from "../shared/Badge";
import { CodeBlock } from "../shared/CodeBlock";
import type { ToolCallPart } from "../../types/chat";

type ToolCallCardProps = {
  part: ToolCallPart;
  status: "pending" | "running" | "resolved" | "error";
}

const statusBadge: Record<ToolCallCardProps["status"], React.ReactNode> = {
  pending:  <Badge variant="dim">pending</Badge>,
  running:  <Badge variant="accent" pulse>running</Badge>,
  resolved: <Badge variant="primary">✓ done</Badge>,
  error:    <Badge variant="error">✕ error</Badge>,
};

export function ToolCallCard({ part, status }: ToolCallCardProps) {
  const header = (
    <span style={{ display: "flex", alignItems: "center", gap: "8px" }}>
      <span style={{ fontFamily: "var(--font-mono)", color: "var(--color-tool)" }}>
        {part.name}
      </span>
      {statusBadge[status]}
    </span>
  );

  return (
    <Collapsible
      header={header}
      defaultOpen={status === "error"}
      accentColor="var(--color-tool)"
    >
      <div style={{ display: "flex", flexDirection: "column", gap: "8px" }}>
        {part.arguments && (
          <div>
            <div style={{ color: "var(--color-text-dim)", fontSize: "var(--font-size-xs)", marginBottom: "4px", fontFamily: "var(--font-mono)" }}>
              Arguments
            </div>
            <CodeBlock
              code={tryPrettyJson(part.arguments)}
              language="json"
            />
          </div>
        )}

        {part.result !== undefined && (
          <div>
            <div
              style={{
                color: part.isError ? "var(--color-error)" : "var(--color-text-dim)",
                fontSize: "var(--font-size-xs)",
                marginBottom: "4px",
                fontFamily: "var(--font-mono)",
              }}
            >
              Result
            </div>
            <CodeBlock code={part.result} />
          </div>
        )}
      </div>
    </Collapsible>
  );
}

function tryPrettyJson(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return s;
  }
}
