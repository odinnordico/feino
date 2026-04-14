/** Blinking caret shown at the end of a streaming response. */
export function StreamingCursor() {
  return (
    <span
      aria-hidden
      style={{
        display: "inline-block",
        width: "2px",
        height: "1em",
        background: "var(--color-primary)",
        marginLeft: "2px",
        verticalAlign: "text-bottom",
        animation: "blink 900ms step-end infinite",
      }}
    />
  );
}
