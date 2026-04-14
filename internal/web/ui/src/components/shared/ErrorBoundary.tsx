import { Component, type ReactNode, type ErrorInfo } from "react";

interface Props {
  children: ReactNode;
  fallback?: ReactNode;
}

interface State {
  error: Error | null;
}

export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("[ErrorBoundary]", error, info.componentStack);
  }

  handleRetry = () => this.setState({ error: null });

  render() {
    if (!this.state.error) return this.props.children;

    if (this.props.fallback) return this.props.fallback;

    return (
      <div
        role="alert"
        style={{
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          height: "100%",
          gap: "16px",
          padding: "40px",
          fontFamily: "var(--font-mono)",
          color: "var(--color-text)",
          textAlign: "center",
        }}
      >
        <div style={{ fontSize: "2rem", color: "var(--color-error)" }}>⚠</div>
        <div style={{ fontSize: "var(--font-size-lg)", color: "var(--color-error)" }}>
          Something went wrong
        </div>
        <pre
          style={{
            background: "var(--color-surface-2)",
            border: "1px solid var(--color-border)",
            borderRadius: "var(--radius-md)",
            padding: "12px 16px",
            fontSize: "var(--font-size-xs)",
            color: "var(--color-text-dim)",
            maxWidth: "600px",
            overflowX: "auto",
            whiteSpace: "pre-wrap",
            wordBreak: "break-word",
            textAlign: "left",
          }}
        >
          {this.state.error.message}
        </pre>
        <button
          onClick={this.handleRetry}
          style={{
            padding: "8px 20px",
            background: "var(--color-primary-muted)",
            border: "1px solid var(--color-primary)",
            borderRadius: "var(--radius-md)",
            color: "var(--color-primary)",
            fontFamily: "var(--font-mono)",
            fontSize: "var(--font-size-sm)",
            cursor: "pointer",
          }}
        >
          Retry
        </button>
      </div>
    );
  }
}
