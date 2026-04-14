import { BarChart, Bar, ResponsiveContainer, Tooltip, Legend, YAxis } from "recharts";
import { useMetricsStore } from "../../store/metricsStore";

export function TokenBarChart() {
  const history = useMetricsStore((s) => s.tokenHistory);

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
        Tokens per turn
      </div>
      <ResponsiveContainer width="100%" height={100}>
        <BarChart data={history} margin={{ top: 2, right: 2, left: 0, bottom: 2 }} barSize={8}>
          <YAxis hide />
          <Tooltip
            contentStyle={{ background: "var(--color-surface-2)", border: "1px solid var(--color-border)", fontSize: "11px", fontFamily: "var(--font-mono)" }}
            labelFormatter={(turn) => `Turn ${turn}`}
          />
          <Legend wrapperStyle={{ fontSize: "10px", color: "var(--color-text-dim)" }} />
          <Bar dataKey="prompt"     fill="var(--color-primary-muted)" name="prompt" />
          <Bar dataKey="completion" fill="var(--color-primary)"       name="completion" />
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}
