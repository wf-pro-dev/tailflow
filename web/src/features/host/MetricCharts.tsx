import { useState } from "react";
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

      {/* Bento grid: 2x2 */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {/* CPU */}
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
            type="area"
            syncId={SYNC_ID}
            showLegend={false}
          />
        </div>

        {/* Memory */}
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
            type="area"
            syncId={SYNC_ID}
            showLegend={false}
          />
        </div>

        {/* Network */}
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

        {/* Disk Usage */}
        <div className="card">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-medium text-ink-secondary">Disk Usage</h3>
          </div>
          {latestDisks.length > 0 ? (
            <div className="space-y-3">
              {latestDisks.map((disk) => (
                <div key={disk.path}>
                  <div className="flex items-center justify-between text-xs">
                    <span className="font-mono text-ink-secondary">{disk.path}</span>
                    <span className="text-ink-muted">
                      {bytes(disk.used)} / {bytes(disk.total)} ({percent(disk.used_pct, 1)})
                    </span>
                  </div>
                  <div className="mt-1.5 h-2 overflow-hidden rounded-full bg-sunken">
                    <div
                      className={`h-full rounded-full transition-all duration-300 ${
                        disk.used_pct > 80 ? "bg-danger" :
                        disk.used_pct > 60 ? "bg-status-degraded" : "bg-status-healthy"
                      }`}
                      style={{ width: `${Math.min(disk.used_pct, 100)}%` }}
                    />
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className="py-8 text-center text-xs text-ink-faint">No disk data</div>
          )}
        </div>
      </div>
    </div>
  );
}
