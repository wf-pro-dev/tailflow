import { useState } from "react";
import { useHostMetrics, useHostMetricsLatest } from "@/lib/api/hooks";
import { useEventStream } from "@/lib/stream/useEventStream";
import { MetricChart } from "@/components/MetricChart";
import { percent, bytes, rate } from "@/lib/format";

const WINDOWS = [
  { label: "5m", value: "5m" },
  { label: "15m", value: "15m" },
  { label: "1h", value: "1h" },
];

export function MetricCharts({ host: hostName }: { host: string }) {
  const [window, setWindow] = useState("15m");
  const { data, isLoading, error } = useHostMetrics(hostName, window);
  // Fetch latest metrics (disks, net interfaces) from /metrics/latest —
  // the /hosts/:host endpoint doesn't include these.
  const { data: latestData } = useHostMetricsLatest(hostName);

  // Subscribe to live metrics.sample events for this host.
  useEventStream([`metrics:${hostName}`]);

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
  if (error) return <div className="card text-red-400">Error: {error.message}</div>;
  if (!data) return null;

  const t = data.t;
  const cpu = data.series.cpu_total ?? [];
  const memPct = data.series.mem_used_pct ?? [];
  const netRx = data.series.net_rx_bps ?? [];
  const netTx = data.series.net_tx_bps ?? [];

  // Get latest disks and network interfaces from /metrics/latest endpoint
  const latestDisks = (latestData as any)?.disks ?? [];
  const latestNetIfaces = (latestData as any)?.net_interfaces ?? [];

  return (
    <div className="space-y-4">
      {/* Window selector + live indicator */}
      <div className="flex items-center justify-between">
        <div className="flex gap-1">
          {WINDOWS.map((w) => (
            <button
              key={w.value}
              onClick={() => setWindow(w.value)}
              className={`btn ${window === w.value ? "btn-primary" : "btn-ghost"}`}
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
            <h3 className="text-sm font-medium text-zinc-300">CPU</h3>
            <span className="text-xs text-zinc-500">
              {cpu.length > 0 ? `${cpu[cpu.length - 1] != null ? percent(cpu[cpu.length - 1] as number, 1) : "—"}` : "—"}
            </span>
          </div>
          <MetricChart
            t={t}
            series={[{ label: "Total", data: cpu, color: "#3DA5E0" }]}
            format={(v) => percent(v, 1)}
            yMax={100}
            type="area"
          />
        </div>

        {/* Memory */}
        <div className="card">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-medium text-zinc-300">Memory</h3>
            <span className="text-xs text-zinc-500">
              {memPct.length > 0 ? `${memPct[memPct.length - 1] != null ? percent(memPct[memPct.length - 1] as number, 1) : "—"}` : "—"}
            </span>
          </div>
          <MetricChart
            t={t}
            series={[{ label: "Used %", data: memPct, color: "#6F3FE0" }]}
            format={(v) => percent(v, 1)}
            yMax={100}
            type="area"
          />
        </div>

        {/* Network */}
        <div className="card">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-medium text-zinc-300">Network</h3>
            <span className="text-xs text-zinc-500">
              {netRx.length > 0 ? `${rate(netRx[netRx.length - 1] as number)}` : "—"}
            </span>
          </div>
          <MetricChart
            t={t}
            series={[
              { label: "RX", data: netRx, color: "#22C55E" },
              { label: "TX", data: netTx, color: "#F59E0B" },
            ]}
            format={(v) => rate(v)}
          />
          {/* Per-interface breakdown */}
          {latestNetIfaces.length > 0 && (
            <div className="mt-3 space-y-1 border-t border-white/[0.04] pt-3">
              <div className="metric-label mb-2">Per Interface</div>
              {latestNetIfaces.map((iface: any) => (
                <div key={iface.name} className="flex items-center justify-between text-xs">
                  <span className="font-mono text-zinc-400">{iface.name}</span>
                  <span className="text-zinc-500">
                    <span className="text-green-400">↓ {rate(iface.rx_bps)}</span>
                    {"  "}
                    <span className="text-amber-400">↑ {rate(iface.tx_bps)}</span>
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Disk Usage */}
        <div className="card">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-medium text-zinc-300">Disk Usage</h3>
          </div>
          {latestDisks.length > 0 ? (
            <div className="space-y-3">
              {latestDisks.map((disk: any) => (
                <div key={disk.path}>
                  <div className="flex items-center justify-between text-xs">
                    <span className="font-mono text-zinc-400">{disk.path}</span>
                    <span className="text-zinc-500">
                      {bytes(disk.used)} / {bytes(disk.total)} ({percent(disk.used_pct, 1)})
                    </span>
                  </div>
                  <div className="mt-1.5 h-2 overflow-hidden rounded-full bg-white/[0.06]">
                    <div
                      className={`h-full rounded-full transition-all duration-300 ${
                        disk.used_pct > 80 ? "bg-red-500" :
                        disk.used_pct > 60 ? "bg-amber-500" : "bg-green-500"
                      }`}
                      style={{ width: `${Math.min(disk.used_pct, 100)}%` }}
                    />
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className="py-8 text-center text-xs text-zinc-600">No disk data</div>
          )}
        </div>
      </div>
    </div>
  );
}
