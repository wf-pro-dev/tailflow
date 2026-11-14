import { useState, useMemo, useEffect } from "react";
import { useHosts } from "@/lib/api/hooks";
import { MetricChart } from "@/components/MetricChart";
import { useChartColors } from "@/lib/chartColors";
import { rate } from "@/lib/format";

interface Sample {
  t: number;
  cpu: number;
  memPct: number;
  netRx: number;
  netTx: number;
  _count: number; // number of hosts aggregated into this bucket
}

const MAX_POINTS = 180;

export function FleetMetrics() {
  const { data: hosts } = useHosts();
  const colors = useChartColors();
  const [samples, setSamples] = useState<Sample[]>([]);
  const [latest, setLatest] = useState<{ cpu: number; mem: number; rx: number; tx: number } | null>(null);

  // Build topics: always include "fleet" so we get fleet.summary events too.
  // The metrics.sample events for each host arrive via the shared useEventStream
  // in FleetOverview — but we need our own listener here because FleetMetrics
  // aggregates raw samples into a local ring buffer (not the query cache).
  const topics = useMemo(() => {
    const t: string[] = [];
    if (hosts) {
      for (const h of hosts) {
        t.push(`metrics:${h.name}`);
      }
    }
    return t;
  }, [hosts?.map(h => h.name).join(",")]); // stable dep on host names

  useEffect(() => {
    if (topics.length === 0) return;

    const topicsParam = topics.join(",");
    const url = `/api/v1/stream?topics=${encodeURIComponent(topicsParam)}`;
    const es = new EventSource(url);

    es.addEventListener("metrics.sample", (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data);
        const t = data.t;
        if (!t) return;

        setSamples((prev) => {
          const last = prev[prev.length - 1];
          if (last && last.t === t) {
            const updated = { ...last };
            updated.cpu = (updated.cpu * updated._count + (data.cpu_total ?? 0)) / (updated._count + 1);
            updated.memPct = (updated.memPct * updated._count + (data.mem_used_pct ?? 0)) / (updated._count + 1);
            updated.netRx += data.net_rx_bps ?? 0;
            updated.netTx += data.net_tx_bps ?? 0;
            updated._count = updated._count + 1;
            return [...prev.slice(0, -1), updated];
          }

          const next: Sample[] = [...prev, {
            t,
            cpu: data.cpu_total ?? 0,
            memPct: data.mem_used_pct ?? 0,
            netRx: data.net_rx_bps ?? 0,
            netTx: data.net_tx_bps ?? 0,
            _count: 1,
          }];
          if (next.length > MAX_POINTS) next.shift();
          return next;
        });

        // Update latest values for at-a-glance display
        setLatest({
          cpu: data.cpu_total ?? 0,
          mem: data.mem_used_pct ?? 0,
          rx: data.net_rx_bps ?? 0,
          tx: data.net_tx_bps ?? 0,
        });
      } catch {
        // ignore
      }
    });

    return () => es.close();
  }, [topics.join(",")]);

  if (samples.length < 2) {
    return (
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        {[0, 1, 2].map((i) => (
          <div key={i} className="card">
            <div className="skeleton h-4 w-24 rounded" />
            <div className="skeleton mt-3 h-32 rounded" />
          </div>
        ))}
      </div>
    );
  }

  const t = samples.map((s) => s.t);
  const cpu = samples.map((s) => s.cpu);
  const mem = samples.map((s) => s.memPct);
  const netRx = samples.map((s) => s.netRx);
  const netTx = samples.map((s) => s.netTx);

  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
      {/* Fleet CPU */}
      <div className="card">
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-sm font-medium text-zinc-300">Fleet CPU</h3>
          <span className="text-lg font-bold tracking-tight tabular-nums text-accent">
            {(latest?.cpu ?? 0).toFixed(1)}%
          </span>
        </div>
        <MetricChart
          t={t}
          series={[{ label: "Mean CPU %", data: cpu, color: colors.accent }]}
          format={(v) => `${v.toFixed(1)}%`}
          yMax={100}
          type="area"
          height={120}
          showLegend={false}
        />
      </div>

      {/* Fleet Memory */}
      <div className="card">
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-sm font-medium text-zinc-300">Fleet Memory</h3>
          <span className="text-lg font-bold tracking-tight tabular-nums text-status-noagent">
            {(latest?.mem ?? 0).toFixed(1)}%
          </span>
        </div>
        <MetricChart
          t={t}
          series={[{ label: "Mean Memory %", data: mem, color: colors.noagent }]}
          format={(v) => `${v.toFixed(1)}%`}
          yMax={100}
          type="area"
          height={120}
          showLegend={false}
        />
      </div>

      {/* Fleet Network */}
      <div className="card">
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-sm font-medium text-zinc-300">Fleet Network</h3>
          <div className="flex gap-3 text-sm">
            <span className="text-status-healthy">↓ {rate(latest?.rx ?? 0)}</span>
            <span className="text-status-degraded">↑ {rate(latest?.tx ?? 0)}</span>
          </div>
        </div>
        <MetricChart
          t={t}
          series={[
            { label: "RX", data: netRx, color: colors.healthy },
            { label: "TX", data: netTx, color: colors.degraded },
          ]}
          format={(v) => rate(v)}
          height={120}
        />
      </div>
    </div>
  );
}
