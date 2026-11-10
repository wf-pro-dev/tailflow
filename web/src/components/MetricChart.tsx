import { AreaChart, Area, LineChart, Line, XAxis, YAxis, CartesianGrid, ChartContainer, ChartTooltip, type ChartConfig } from "@/components/ui/chart";

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

  // Build chart config from series
  const chartConfig: ChartConfig = {};
  for (const s of series) {
    chartConfig[s.label] = { label: s.label, color: s.color };
  }

  const formatXAxis = (ts: number) => {
    const d = new Date(ts * 1000);
    return `${d.getHours().toString().padStart(2, "0")}:${d.getMinutes().toString().padStart(2, "0")}`;
  };

  const formatY = (v: number) => format ? format(v) : String(v);

  if (type === "area") {
    return (
      <ChartContainer config={chartConfig} className="w-full" style={{ height }}>
        <AreaChart data={chartData} margin={{ top: 5, right: 10, left: 0, bottom: 5 }}>
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
            tickFormatter={formatY}
          />
          <ChartTooltip />
          {series.map((s) => (
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
          ))}
        </AreaChart>
      </ChartContainer>
    );
  }

  return (
    <ChartContainer config={chartConfig} className="w-full" style={{ height }}>
      <LineChart data={chartData} margin={{ top: 5, right: 10, left: 0, bottom: 5 }}>
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
          tickFormatter={formatY}
        />
        <ChartTooltip />
        {series.map((s) => (
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
        ))}
      </LineChart>
    </ChartContainer>
  );
}
