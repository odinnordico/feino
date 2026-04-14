import { Area, AreaChart, ResponsiveContainer, Tooltip, YAxis } from "recharts";
import { useMetricsStore } from "../../store/metricsStore";
import { formatMs } from "../../lib/utils";

export function LatencySparkline() {
  const history = useMetricsStore((s) => s.latencyHistory);

  if (history.length === 0) {
    return (
      <div style={{ color: "var(--color-text-faint)", fontSize: "var(--font-size-xs)", textAlign: "center", padding: "20px 0" }}>
        No data yet
      </div>
    );
  }

  return (
    <div>
      <div style={{ color: "var(--color-text-dim)", fontSize: "var(--font-size-xs)", fontFamily: "var(--font-mono)", marginBottom: "6px" }}>
        Latency (last {history.length} turns)
      </div>
      <ResponsiveContainer width="100%" height={80}>
        <AreaChart data={history} margin={{ top: 2, right: 2, left: 0, bottom: 2 }}>
          <defs>
            <linearGradient id="latencyGrad" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%"  stopColor="var(--color-primary)" stopOpacity={0.3} />
              <stop offset="95%" stopColor="var(--color-primary)" stopOpacity={0.02} />
            </linearGradient>
          </defs>
          <YAxis hide domain={["auto", "auto"]} />
          <Tooltip
            contentStyle={{ background: "var(--color-surface-2)", border: "1px solid var(--color-border)", fontSize: "11px", fontFamily: "var(--font-mono)" }}
            formatter={(v: number) => [formatMs(v), "latency"]}
            labelFormatter={(turn) => `Turn ${turn}`}
          />
          <Area type="monotone" dataKey="ms" stroke="var(--color-primary)" strokeWidth={1.5} fill="url(#latencyGrad)" dot={false} />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}
