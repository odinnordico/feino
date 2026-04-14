import { useMetricsStore } from "../../store/metricsStore";
import { useSessionStore } from "../../store/sessionStore";
import { LatencySparkline } from "./LatencySparkline";
import { TokenBarChart } from "./TokenBarChart";
import { formatMs, formatTokens } from "../../lib/utils";
import { useWindowWidth } from "../../hooks/useWindowWidth";

function MetricsPanelContent({ onClose }: { onClose?: () => void }) {
  const usage = useMetricsStore((s) => s.currentUsage);

  return (
    <>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
        <div style={{ color: "var(--color-primary)", fontFamily: "var(--font-mono)", fontSize: "var(--font-size-sm)", fontWeight: 600 }}>
          ⊞ Metrics
        </div>
        {onClose && (
          <button
            onClick={onClose}
            aria-label="Close metrics panel"
            style={{ background: "none", border: "none", cursor: "pointer", color: "var(--color-text-dim)", fontSize: "1.1rem", lineHeight: 1 }}
          >
            ×
          </button>
        )}
      </div>

      <LatencySparkline />
      <TokenBarChart />

      {usage && (
        <div style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
          <div style={{ color: "var(--color-text-dim)", fontSize: "var(--font-size-xs)", fontFamily: "var(--font-mono)" }}>
            Last turn
          </div>
          {[
            ["Prompt",     `${formatTokens(usage.promptTokens)} tokens`],
            ["Completion", `${formatTokens(usage.completionTokens)} tokens`],
            ["Total",      `${formatTokens(usage.totalTokens)} tokens`],
            ["Duration",   formatMs(usage.durationMs)],
          ].map(([label, value]) => (
            <div key={label} style={{ display: "flex", justifyContent: "space-between", fontSize: "var(--font-size-xs)", fontFamily: "var(--font-mono)" }}>
              <span style={{ color: "var(--color-text-dim)" }}>{label}</span>
              <span style={{ color: "var(--color-primary)" }}>{value}</span>
            </div>
          ))}
        </div>
      )}
    </>
  );
}

export function MetricsPanel() {
  const metricsOpen   = useSessionStore((s) => s.metricsOpen);
  const toggleMetrics = useSessionStore((s) => s.toggleMetrics);
  const windowWidth   = useWindowWidth();
  const isMobile      = windowWidth < 768;

  if (!metricsOpen) return null;

  // Mobile: slide-up drawer
  if (isMobile) {
    return (
      <>
        {/* Backdrop */}
        <div
          onClick={toggleMetrics}
          style={{
            position: "fixed", inset: 0, zIndex: 400,
            background: "rgba(0,0,0,0.5)",
          }}
        />
        <aside
          role="dialog"
          aria-label="Metrics panel"
          aria-modal="true"
          style={{
            position: "fixed",
            bottom: 0, left: 0, right: 0,
            zIndex: 401,
            background: "var(--color-surface-1)",
            borderTop: "1px solid var(--color-border)",
            borderRadius: "var(--radius-lg) var(--radius-lg) 0 0",
            padding: "16px 16px 32px",
            display: "flex",
            flexDirection: "column",
            gap: "20px",
            maxHeight: "70vh",
            overflowY: "auto",
            animation: "fadeSlideUp var(--transition-base) ease both",
          }}
        >
          <MetricsPanelContent onClose={toggleMetrics} />
        </aside>
      </>
    );
  }

  // Desktop / tablet: side panel
  return (
    <aside
      role="complementary"
      aria-label="Metrics panel"
      style={{
        width: "260px",
        flexShrink: 0,
        borderLeft: "1px solid var(--color-border)",
        background: "var(--color-surface-1)",
        display: "flex",
        flexDirection: "column",
        padding: "16px 12px",
        gap: "20px",
        overflowY: "auto",
      }}
    >
      <MetricsPanelContent />
    </aside>
  );
}
