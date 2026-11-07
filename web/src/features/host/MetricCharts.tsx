import { useState } from "react";
import { useHostMetrics } from "@/lib/api/hooks";
import { useEventStream } from "@/lib/stream/useEventStream";
import { MetricChart } from "@/components/MetricChart";
import { percent, rate } from "@/lib/format";

const WINDOWS = [
  { label: "5m", value: "5m" },
  { label: "15m", value: "15m" },
  { label: "1h", value: "1h" },
];

export function MetricCharts({ host: hostName }: { host: string }) {
  const [window, setWindow] = useState("15m");
  const { data, isLoading, error } = useHostMetrics(hostName, window);

  // Subscribe to live metrics.sample events for this host.
  useEventStream([`metrics:${hostName}`]);

  if (isLoading) return <div className="text-gray-500">Loading metrics…</div>;
  if (error) return <div className="text-red-600">Error: {error.message}</div>;
  if (!data) return null;

  const t = data.t;
  const cpu = data.series.cpu_total ?? [];
  const memPct = data.series.mem_used_pct ?? [];
  const netRx = data.series.net_rx_bps ?? [];
  const netTx = data.series.net_tx_bps ?? [];

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex gap-1">
          {WINDOWS.map((w) => (
            <button
              key={w.value}
              onClick={() => setWindow(w.value)}
              className={`btn ${window === w.value ? "bg-blue-600 text-white dark:bg-blue-600 dark:text-white" : ""}`}
            >
              {w.label}
            </button>
          ))}
        </div>
      </div>

      <div className="card">
        <h3 className="mb-2 text-sm font-medium">CPU</h3>
        <MetricChart
          t={t}
          series={[{ label: "Total", data: cpu, color: "#2563eb" }]}
          format={(v) => percent(v, 1)}
          yMax={100}
        />
      </div>

      <div className="card">
        <h3 className="mb-2 text-sm font-medium">Memory</h3>
        <MetricChart
          t={t}
          series={[{ label: "Used %", data: memPct, color: "#9333ea" }]}
          format={(v) => percent(v, 1)}
          yMax={100}
        />
      </div>

      <div className="card">
        <h3 className="mb-2 text-sm font-medium">Network</h3>
        <MetricChart
          t={t}
          series={[
            { label: "RX", data: netRx, color: "#16a34a" },
            { label: "TX", data: netTx, color: "#d97706" },
          ]}
          format={(v) => rate(v)}
        />
      </div>
    </div>
  );
}
