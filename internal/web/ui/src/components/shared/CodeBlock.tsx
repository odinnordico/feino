import { useState } from "react";

type CodeBlockProps = {
  code: string;
  language?: string;
  maxLines?: number;
}

const CHAR_LIMIT = 2000;

export function CodeBlock({ code, language, maxLines }: CodeBlockProps) {
  const [expanded, setExpanded] = useState(false);

  const truncated = !expanded && code.length > CHAR_LIMIT;
  const display = truncated ? code.slice(0, CHAR_LIMIT) : code;

  const linesStyle: React.CSSProperties =
    !expanded && maxLines
      ? {
          maxHeight: `${maxLines * 1.5}em`,
          overflow: "hidden",
        }
      : {};

  return (
    <div style={{ position: "relative" }}>
      <pre
        style={{
          background: "var(--color-surface-2)",
          border: "1px solid var(--color-border)",
          borderRadius: "var(--radius-md)",
          padding: "12px",
          overflow: "auto",
          fontFamily: "var(--font-mono)",
          fontSize: "var(--font-size-sm)",
          color: "var(--color-text)",
          margin: 0,
          ...linesStyle,
        }}
      >
        {language && (
          <span
            style={{
              position: "absolute",
              top: "6px",
              right: "8px",
              color: "var(--color-text-faint)",
              fontSize: "0.7rem",
              fontFamily: "var(--font-mono)",
            }}
          >
            {language}
          </span>
        )}
        <code>{display}</code>
      </pre>

      {truncated && (
        <button
          onClick={() => setExpanded(true)}
          style={{
            display: "block",
            width: "100%",
            padding: "4px",
            background: "var(--color-surface-2)",
            border: "1px solid var(--color-border)",
            borderTop: "none",
            borderRadius: "0 0 var(--radius-md) var(--radius-md)",
            color: "var(--color-accent)",
            fontSize: "var(--font-size-xs)",
            cursor: "pointer",
            fontFamily: "var(--font-mono)",
          }}
        >
          Show full output ({code.length.toLocaleString()} chars)
        </button>
      )}
    </div>
  );
}
