import {
  AreaChart, Area, LineChart, Line, XAxis, YAxis, CartesianGrid,
  Tooltip, ResponsiveContainer, type TooltipProps,
} from "recharts";

interface Series {
  label: string;
  data: (number | null)[];
  color: string;
}

interface Props {
  t: number[];
  series: Series[];
  format?: (v: number) => string;
  yMax?: number;
  height?: number;
  type?: "line" | "area";
}

export function MetricChart({ t, series, format, yMax, height = 200, type = "line" }: Props) {
  // Build chart data: array of { t, series1, series2, ... }
  const chartData = t.map((timestamp, i) => {
    const point: Record<string, number | null> = { t: timestamp };
    for (const s of series) {
      point[s.label] = s.data[i] ?? null;
    }
    return point;
  });

  const formatTooltip = (value: number | null, name: string) => {
    if (value == null) return ["—", name];
    return [format ? format(value) : String(value), name];
  };

  const formatXAxis = (ts: number) => {
    const d = new Date(ts * 1000);
    return `${d.getHours().toString().padStart(2, "0")}:${d.getMinutes().toString().padStart(2, "0")}`;
  };

  const ChartComponent = type === "area" ? AreaChart : LineChart;

  return (
    <ResponsiveContainer width="100%" height={height}>
      <ChartComponent data={chartData} margin={{ top: 5, right: 10, left: 0, bottom: 5 }}>
        <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.04)" />
        <XAxis
          dataKey="t"
          tickFormatter={formatXAxis}
          stroke="#52525B"
          tick={{ fontSize: 11, fill: "#71717A" }}
          tickLine={false}
          axisLine={false}
        />
        <YAxis
          stroke="#52525B"
          tick={{ fontSize: 11, fill: "#71717A" }}
          tickLine={false}
          axisLine={false}
          width={50}
          domain={yMax ? [0, yMax] : ["auto", "auto"]}
          tickFormatter={(v: number) => format ? format(v) : String(v)}
        />
        <Tooltip
          formatter={formatTooltip as any}
          labelFormatter={(label: number) => {
            const d = new Date(label * 1000);
            return d.toLocaleTimeString();
          }}
          contentStyle={{
            background: "#18181B",
            border: "1px solid rgba(255,255,255,0.08)",
            borderRadius: "8px",
            fontSize: "12px",
            color: "#FAFAFA",
          }}
          labelStyle={{ color: "#71717A", marginBottom: "4px" }}
        />
        {series.map((s) =>
          type === "area" ? (
            <Area
              key={s.label}
              type="monotone"
              dataKey={s.label}
              stroke={s.color}
              fill={s.color}
              fillOpacity={0.1}
              strokeWidth={1.5}
              dot={false}
              connectNulls
            />
          ) : (
            <Line
              key={s.label}
              type="monotone"
              dataKey={s.label}
              stroke={s.color}
              strokeWidth={1.5}
              dot={false}
              connectNulls
            />
          )
        )}
      </ChartComponent>
    </ResponsiveContainer>
  );
}
