import { useState } from "react";
import { AreaChart, Area, LineChart, Line, XAxis, YAxis, CartesianGrid, ChartContainer, ChartTooltip, type ChartConfig } from "@/components/ui/chart";
import { useChartColors } from "@/lib/chartColors";

export interface ChartSeries {
  label: string;
  data: (number | null)[];
  color: string;
}

interface Props {
  t: number[];
  series: ChartSeries[];
  format?: (v: number) => string;
  yMax?: number;
  height?: number;
  type?: "line" | "area";
  /** Shared crosshair sync id — charts with the same syncId move their
   *  tooltip/crosshair together. */
  syncId?: string;
  /** Show the clickable legend (per-series toggle). Defaults to true when >1 series. */
  showLegend?: boolean;
}

export function MetricChart({ t, series, format, yMax, height = 200, type = "line", syncId, showLegend }: Props) {
  const colors = useChartColors();
  const [hidden, setHidden] = useState<Set<string>>(new Set());
  const legend = showLegend ?? series.length > 1;

  const visible = series.filter((s) => !hidden.has(s.label));

  const chartData = t.map((timestamp, i) => {
    const point: Record<string, number | null> = { t: timestamp };
    for (const s of visible) {
      point[s.label] = s.data[i] ?? null;
    }
    return point;
  });

  const chartConfig: ChartConfig = {};
  for (const s of series) {
    chartConfig[s.label] = { label: s.label, color: s.color };
  }

  const formatXAxis = (ts: number) => {
    const d = new Date(ts * 1000);
    return `${d.getHours().toString().padStart(2, "0")}:${d.getMinutes().toString().padStart(2, "0")}`;
  };

  const formatY = (v: number) => (format ? format(v) : String(v));

  const toggle = (label: string) => {
    setHidden((prev) => {
      const next = new Set(prev);
      if (next.has(label)) next.delete(label);
      else next.add(label);
      // Never hide the last visible series.
      if (series.length - next.size < 1) return prev;
      return next;
    });
  };

  const Chart = type === "area" ? AreaChart : LineChart;
  const chartProps: Record<string, unknown> = { data: chartData, margin: { top: 5, right: 10, left: 0, bottom: 5 } };
  if (syncId) chartProps.syncId = syncId;

  return (
    <div className="w-full">
      {legend && (
        <div className="mb-2 flex flex-wrap items-center gap-x-4 gap-y-1">
          {series.map((s) => {
            const isHidden = hidden.has(s.label);
            return (
              <button
                key={s.label}
                onClick={() => toggle(s.label)}
                className="flex items-center gap-1.5 text-xs transition-opacity hover:opacity-80"
                style={{ color: "var(--ink-secondary)" }}
              >
                <span
                  className="h-2.5 w-2.5 rounded-sm"
                  style={{ backgroundColor: s.color, opacity: isHidden ? 0.3 : 1 }}
                />
                <span className={isHidden ? "line-through text-ink-faint" : ""}>{s.label}</span>
              </button>
            );
          })}
        </div>
      )}
      <ChartContainer config={chartConfig} className="w-full" style={{ height }}>
        <Chart {...chartProps}>
          <CartesianGrid strokeDasharray="3 3" stroke={colors.grid} vertical={false} />
          <XAxis
            dataKey="t"
            tickFormatter={formatXAxis}
            stroke={colors.axis}
            tick={{ fontSize: 11, fill: colors.tick }}
            tickLine={false}
            axisLine={false}
            minTickGap={40}
          />
          <YAxis
            stroke={colors.axis}
            tick={{ fontSize: 11, fill: colors.tick }}
            tickLine={false}
            axisLine={false}
            width={48}
            domain={yMax ? [0, yMax] : ["auto", "auto"]}
            tickFormatter={formatY}
          />
          <ChartTooltip />
          {visible.map((s) =>
            type === "area" ? (
              <Area
                key={s.label}
                type="monotone"
                dataKey={s.label}
                stroke={s.color}
                fill={s.color}
                fillOpacity={0.1}
                isAnimationActive={false}
                strokeWidth={1.5}
                dot={false}
                connectNulls
              />
            ) : (
              <Line
                key={s.label}
                type="monotone"
                dataKey={s.label}
                isAnimationActive={false}
                stroke={s.color}
                strokeWidth={1.5}
                dot={false}
                connectNulls
              />
            ),
          )}
        </Chart>
      </ChartContainer>
    </div>
  );
}
