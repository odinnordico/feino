type SkeletonProps = {
  width?: string | number;
  height?: string | number;
  borderRadius?: string;
  style?: React.CSSProperties;
}

export function Skeleton({ width = "100%", height = "16px", borderRadius = "var(--radius-sm)", style }: SkeletonProps) {
  return (
    <div
      aria-hidden="true"
      style={{
        width,
        height,
        borderRadius,
        background: "linear-gradient(90deg, var(--color-surface-2) 25%, var(--color-surface-3) 50%, var(--color-surface-2) 75%)",
        backgroundSize: "200% 100%",
        animation: "shimmer 1.4s ease infinite",
        ...style,
      }}
    />
  );
}

/** A stack of skeleton lines — convenience for text-like placeholders. */
export function SkeletonLines({ count = 3, lastWidth = "60%" }: { count?: number; lastWidth?: string }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "8px" }}>
      {Array.from({ length: count }).map((_, i) => (
        <Skeleton key={i} width={i === count - 1 ? lastWidth : "100%"} />
      ))}
    </div>
  );
}

/** A card-shaped skeleton block. */
export function SkeletonCard({ height = "80px" }: { height?: string }) {
  return (
    <Skeleton
      height={height}
      borderRadius="var(--radius-md)"
      style={{ marginBottom: "2px" }}
    />
  );
}
