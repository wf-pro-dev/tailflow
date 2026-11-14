import { useId } from "react";

/*
 * Sparkline — a tiny inline SVG trace for table rows. Deliberately NOT
 * Recharts (too heavy for N rows). Renders a normalized polyline plus a
 * soft area fill. Null/undefined points are skipped (gaps).
 */

interface Props {
  data: (number | null)[];
  color?: string;
  width?: number;
  height?: number;
  /** Optional second series drawn thinner (e.g. mem under cpu). */
  data2?: (number | null)[];
  color2?: string;
}

export function Sparkline({
  data,
  color = "var(--accent)",
  width = 80,
  height = 28,
  data2,
  color2 = "var(--status-degraded)",
}: Props) {
  const gradId = useId();
  const clean = data.filter((v): v is number => v != null && !isNaN(v));
  if (clean.length < 2) {
    return <div style={{ width, height }} className="shrink-0" />;
  }

  const min = Math.min(...clean, ...(data2?.filter((v): v is number => v != null && !isNaN(v)) ?? []));
  const max = Math.max(...clean, ...(data2?.filter((v): v is number => v != null && !isNaN(v)) ?? []));
  const range = max - min || 1;

  const toPath = (vals: (number | null)[], h: number, w: number) => {
    const pts: string[] = [];
    let idx = 0;
    for (let i = 0; i < vals.length; i++) {
      const v = vals[i];
      if (v == null || isNaN(v)) continue;
      const x = (idx / (clean.length - 1)) * w;
      const y = h - ((v - min) / range) * (h - 2) - 1;
      pts.push(`${idx === 0 ? "M" : "L"}${x.toFixed(1)} ${y.toFixed(1)}`);
      idx++;
    }
    return pts.join(" ");
  };

  const linePath = toPath(data, height, width);
  const areaPath = `${linePath} L${width} ${height} L0 ${height} Z`;

  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} className="shrink-0 overflow-visible">
      <defs>
        <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity="0.28" />
          <stop offset="100%" stopColor={color} stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={areaPath} fill={`url(#${gradId})`} />
      <path d={linePath} fill="none" stroke={color} strokeWidth="1.25" strokeLinejoin="round" strokeLinecap="round" />
      {data2 && (
        <path d={toPath(data2, height, width)} fill="none" stroke={color2} strokeWidth="1" strokeOpacity="0.7" strokeLinejoin="round" />
      )}
    </svg>
  );
}
