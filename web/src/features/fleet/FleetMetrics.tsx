import { useEffect, useRef, useState, useMemo } from "react";
import { useHosts } from "@/lib/api/hooks";
import { MetricChart } from "@/components/MetricChart";
import { percent, rate } from "@/lib/format";

interface Sample {
  t: number;
  cpu: number;
  memPct: number;
  netRx: number;
  netTx: number;
}

const MAX_POINTS = 180; // ~15 min at 5s step

export function FleetMetrics() {
  const { data: hosts } = useHosts();
  const [samples, setSamples] = useState<Sample[]>([]);
  const esRef = useRef<EventSource | null>(null);

  // Build the topics string from the host list
  const topics = useMemo(() => {
    if (!hosts || hosts.length === 0) return [];
    return hosts.map((h) => `metrics:${h.name}`);
  }, [hosts]);

  // Subscribe to all hosts' metrics.sample events
  useEffect(() => {
    if (topics.length === 0) return;

    const topicsParam = topics.join(",");
    const url = `/api/v1/stream?topics=${encodeURIComponent(topicsParam)}`;
    const es = new EventSource(url);
    esRef.current = es;

    es.addEventListener("metrics.sample", (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data);
        const t = data.t;
        if (!t) return;

        // Accumulate this sample into the current time bucket
        setSamples((prev) => {
          // If the last sample has the same timestamp, accumulate into it
          const last = prev[prev.length - 1];
          if (last && last.t === t) {
            const updated = { ...last };
            updated.cpu = (updated.cpu * (updated._count || 1) + (data.cpu_total ?? 0)) / ((updated._count || 1) + 1);
            updated.memPct = (updated.memPct * (updated._count || 1) + (data.mem_used_pct ?? 0)) / ((updated._count || 1) + 1);
            updated.netRx += data.net_rx_bps ?? 0;
            updated.netTx += data.net_tx_bps ?? 0;
            updated._count = (updated._count || 1) + 1;
            return [...prev.slice(0, -1), updated];
          }

          // New time bucket
          const next = [...prev, {
            t,
            cpu: data.cpu_total ?? 0,
            memPct: data.mem_used_pct ?? 0,
            netRx: data.net_rx_bps ?? 0,
            netTx: data.net_tx_bps ?? 0,
            _count: 1,
          } as Sample & { _count: number }];
          if (next.length > MAX_POINTS) next.shift();
          return next;
        });
      } catch {
        // ignore
      }
    });

    return () => {
      es.close();
      esRef.current = null;
    };
  }, [topics.join(",")]);

  if (samples.length < 2) {
    return null; // Don't render until we have enough data
  }

  const t = samples.map((s) => s.t);
  const cpu = samples.map((s) => s.cpu);
  const mem = samples.map((s) => s.memPct);
  const netRx = samples.map((s) => s.netRx);
  const netTx = samples.map((s) => s.netTx);

  const latestCpu = cpu[cpu.length - 1] ?? 0;
  const latestMem = mem[mem.length - 1] ?? 0;
  const latestRx = netRx[netRx.length - 1] ?? 0;
  const latestTx = netTx[netTx.length - 1] ?? 0;

  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
      {/* Fleet CPU */}
      <div className="card">
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-sm font-medium text-zinc-300">Fleet CPU</h3>
          <span className="text-lg font-bold tracking-tight text-blue-400">
            {latestCpu.toFixed(1)}%
          </span>
        </div>
        <MetricChart
          t={t}
          series={[{ label: "Mean CPU %", data: cpu, color: "#3DA5E0" }]}
          format={(v) => `${v.toFixed(1)}%`}
          yMax={100}
          type="area"
          height={140}
        />
      </div>

      {/* Fleet Memory */}
      <div className="card">
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-sm font-medium text-zinc-300">Fleet Memory</h3>
          <span className="text-lg font-bold tracking-tight text-purple-400">
            {latestMem.toFixed(1)}%
          </span>
        </div>
        <MetricChart
          t={t}
          series={[{ label: "Mean Memory %", data: mem, color: "#6F3FE0" }]}
          format={(v) => `${v.toFixed(1)}%`}
          yMax={100}
          type="area"
          height={140}
        />
      </div>

      {/* Fleet Network */}
      <div className="card">
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-sm font-medium text-zinc-300">Fleet Network</h3>
          <div className="flex gap-3 text-sm">
            <span className="text-green-400">↓ {rate(latestRx)}</span>
            <span className="text-amber-400">↑ {rate(latestTx)}</span>
          </div>
        </div>
        <MetricChart
          t={t}
          series={[
            { label: "RX", data: netRx, color: "#22C55E" },
            { label: "TX", data: netTx, color: "#F59E0B" },
          ]}
          format={(v) => rate(v)}
          height={140}
        />
      </div>
    </div>
  );
}
