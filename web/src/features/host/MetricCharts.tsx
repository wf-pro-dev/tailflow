import { useState } from "react";
import { BarChart, Bar, Cell, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from "recharts";
import { useHostMetrics, useHostMetricsLatest } from "@/lib/api/hooks";
import { MetricChart } from "@/components/MetricChart";
import { useChartColors } from "@/lib/chartColors";
import { percent, bytes, rate } from "@/lib/format";

const WINDOWS = [
  { label: "5m", value: "5m" },
  { label: "15m", value: "15m" },
  { label: "1h", value: "1h" },
];

// All metric charts on this page share a crosshair — hover one and the guide
// line appears at the same timestamp across all of them.
const SYNC_ID = "host-metrics";

/*
 * MetricCharts — the host-detail metric grid, redesigned so each metric gets
 * the chart type that actually fits it (one right chart per metric), instead
 * of every metric collapsing into an area:
 *   - CPU      : line + 80/90% threshold reference lines
 *   - Memory   : line + 80/90% threshold reference lines
 *   - Network  : two-line (RX vs TX) + per-interface breakdown
 *   - Disk     : horizontal bar chart per mountpoint, threshold-coloured cells
 *                (replaces the static progress bars)
 */
export function MetricCharts({ host: hostName }: { host: string }) {
  const [window, setWindow] = useState("15m");
  const { data, isLoading, error } = useHostMetrics(hostName, window);
  const { data: latestData } = useHostMetricsLatest(hostName);
  const colors = useChartColors();

  if (isLoading) {
    return (
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="card">
            <div className="skeleton h-4 w-20 rounded" />
            <div className="skeleton mt-3 h-48 rounded" />
          </div>
        ))}
      </div>
    );
  }
  if (error) return <div className="card text-danger">Error: {error.message}</div>;
  if (!data) return null;

  const t = data.t;
  const cpu = data.series.cpu_total ?? [];
  const memPct = data.series.mem_used_pct ?? [];
  const netRx = data.series.net_rx_bps ?? [];
  const netTx = data.series.net_tx_bps ?? [];

  const latestDisks = latestData?.disks ?? [];
  const latestNetIfaces = latestData?.net_interfaces ?? [];

  const last = (a: (number | null)[]) => (a.length > 0 ? a[a.length - 1] : null);

  // 80/90% thresholds for CPU and Memory.
  const thresholds = [
    { y: 80, color: colors.stale, label: "80" },
    { y: 90, color: colors.danger, label: "90" },
  ];

  return (
    <div className="space-y-4">
      {/* Window selector + live indicator */}
      <div className="flex items-center justify-between">
        <div className="flex gap-1">
          {WINDOWS.map((w) => (
            <button
              key={w.value}
              onClick={() => setWindow(w.value)}
              className={`seg ${window === w.value ? "seg-active" : ""}`}
            >
              {w.label}
            </button>
          ))}
        </div>
        <div className="live-indicator">Live</div>
      </div>

      {/* Bento grid: 2x2 — one right chart per metric */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {/* CPU — line + thresholds */}
        <div className="card">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-medium text-ink-secondary">CPU</h3>
            <span className="text-xs tabular-nums text-ink-muted">
              {cpu.length > 0 ? (last(cpu) != null ? percent(last(cpu) as number, 1) : "—") : "—"}
            </span>
          </div>
          <MetricChart
            t={t}
            series={[{ label: "Total", data: cpu, color: colors.accent }]}
            format={(v) => percent(v, 1)}
            yMax={100}
            type="line"
            syncId={SYNC_ID}
            showLegend={false}
            referenceLines={thresholds}
          />
        </div>

        {/* Memory — line + thresholds */}
        <div className="card">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-medium text-ink-secondary">Memory</h3>
            <span className="text-xs tabular-nums text-ink-muted">
              {memPct.length > 0 ? (last(memPct) != null ? percent(last(memPct) as number, 1) : "—") : "—"}
            </span>
          </div>
          <MetricChart
            t={t}
            series={[{ label: "Used %", data: memPct, color: colors.noagent }]}
            format={(v) => percent(v, 1)}
            yMax={100}
            type="line"
            syncId={SYNC_ID}
            showLegend={false}
            referenceLines={thresholds}
          />
        </div>

        {/* Network — two-line (RX vs TX) */}
        <div className="card">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-medium text-ink-secondary">Network</h3>
            <span className="text-xs tabular-nums text-ink-muted">
              {netRx.length > 0 ? rate(last(netRx) as number) : "—"}
            </span>
          </div>
          <MetricChart
            t={t}
            series={[
              { label: "RX", data: netRx, color: colors.healthy },
              { label: "TX", data: netTx, color: colors.degraded },
            ]}
            format={(v) => rate(v)}
            syncId={SYNC_ID}
          />
          {/* Per-interface breakdown */}
          {latestNetIfaces.length > 0 && (
            <div className="mt-3 space-y-1 border-t border-line-subtle pt-3">
              <div className="metric-label mb-2">Per Interface</div>
              {latestNetIfaces.map((iface) => (
                <div key={iface.name} className="flex items-center justify-between text-xs">
                  <span className="font-mono text-ink-secondary">{iface.name}</span>
                  <span className="text-ink-muted">
                    <span className="text-status-healthy">↓ {rate(iface.rx_bps)}</span>
                    {"  "}
                    <span className="text-status-degraded">↑ {rate(iface.tx_bps)}</span>
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Disk — horizontal bar chart per mountpoint */}
        <div className="card">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-medium text-ink-secondary">Disk Usage</h3>
          </div>
          <DiskUsageBars disks={latestDisks} />
        </div>
      </div>
    </div>
  );
}

/*
 * DiskUsageBars — one horizontal bar per mountpoint, length = used %, fill
 * coloured by threshold (green / amber / red). A bar chart compares mounts at
 * a glance better than a stack of separate progress bars, and the colour
 * encoding carries the triage signal without a separate legend.
 */
function DiskUsageBars({ disks }: { disks: { path: string; total: number; used: number; used_pct: number }[] }) {
  const colors = useChartColors();

  if (disks.length === 0) {
    return <div className="py-8 text-center text-xs text-ink-faint">No disk data</div>;
  }

  const data = disks.map((d) => ({ path: d.path, used: d.used_pct, usedBytes: d.used, total: d.total }));
  const cellColor = (pct: number) => (pct > 80 ? colors.danger : pct > 60 ? colors.degraded : colors.healthy);
  const height = Math.max(120, disks.length * 34 + 24);

  return (
    <div style={{ height }}>
      <ResponsiveContainer width="100%" height="100%">
        <BarChart data={data} layout="vertical" margin={{ top: 4, right: 16, left: 0, bottom: 4 }}>
          <CartesianGrid strokeDasharray="3 3" stroke={colors.grid} horizontal={false} />
          <XAxis
            type="number"
            domain={[0, 100]}
            stroke={colors.axis}
            tick={{ fontSize: 11, fill: colors.tick }}
            tickFormatter={(v: number) => `${v}%`}
            tickLine={false}
            axisLine={false}
          />
          <YAxis
            type="category"
            dataKey="path"
            stroke={colors.axis}
            tick={{ fontSize: 11, fill: colors.tick }}
            width={120}
            tickLine={false}
            axisLine={false}
          />
          <Tooltip cursor={{ fill: "transparent" }} content={<DiskTooltip />} />
          <Bar dataKey="used" radius={[0, 3, 3, 0]} isAnimationActive={false}>
            {data.map((d, i) => (
              <Cell key={i} fill={cellColor(d.used)} />
            ))}
          </Bar>
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}

interface DiskTooltipProps {
  active?: boolean;
  payload?: { payload: { path: string; used: number; usedBytes: number; total: number } }[];
}

function DiskTooltip({ active, payload }: DiskTooltipProps) {
  if (!active || !payload?.length) return null;
  const d = payload[0].payload;
  return (
    <div className="rounded-lg border border-line bg-surface px-3 py-2 text-xs shadow-xl">
      <div className="font-mono text-zinc-300">{d.path}</div>
      <div className="mt-1 text-zinc-400">
        {bytes(d.usedBytes)} / {bytes(d.total)} · <span className="text-zinc-200">{d.used.toFixed(1)}%</span>
      </div>
    </div>
  );
}
