/*
 * DiffChip — shows the delta between the latest value and the previous sample.
 * Static numbers read "generated"; deltas read "alive". Directional color:
 *   - higher-is-worse (cpu, mem): rising → degraded, falling → healthy
 *   - higher-is-better: rising → healthy, falling → degraded
 *   - neutral: muted
 */

type Polarity = "higher-is-worse" | "higher-is-better" | "neutral";

interface Props {
  current: number | null | undefined;
  previous: number | null | undefined;
  /** Format the absolute delta value. */
  format?: (delta: number) => string;
  polarity?: Polarity;
  /** Treat |delta| below this as flat (no arrow, muted). */
  threshold?: number;
}

export function DiffChip({
  current,
  previous,
  format = (d) => `${d > 0 ? "+" : ""}${d.toFixed(1)}`,
  polarity = "neutral",
  threshold = 0.01,
}: Props) {
  if (current == null || previous == null || isNaN(current) || isNaN(previous)) {
    return null;
  }
  const delta = current - previous;
  if (Math.abs(delta) < threshold) {
    return <span className="text-[11px] tabular-nums text-ink-faint">—</span>;
  }

  const up = delta > 0;
  let color = "text-ink-muted";
  if (polarity === "higher-is-worse") color = up ? "text-status-degraded" : "text-status-healthy";
  else if (polarity === "higher-is-better") color = up ? "text-status-healthy" : "text-status-degraded";

  const arrow = up ? "▲" : "▼";

  return (
    <span className={`inline-flex items-center gap-0.5 text-[11px] tabular-nums ${color}`}>
      <span className="text-[9px]">{arrow}</span>
      {format(Math.abs(delta))}
    </span>
  );
}
