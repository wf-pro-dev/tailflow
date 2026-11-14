import { Sparkline } from "@/components/Sparkline";
import { percent, bytes, rate } from "@/lib/format";
import type { FleetSummary } from "@/lib/api/client";
import type { FleetSample } from "./useFleetSamples";

/*
 * FleetKpiStrip — the Overview hero. Threshold-coloured stat tiles, each
 * carrying a value, a one-line sub, and (where the metric is a live time
 * series) a stream-driven sparkline that follows the fleet sample ring.
 *
 * Replaces the old FleetSummaryCards (which showed only static snapshot values)
 * and absorbs the trend hint that the removed FleetMetrics mean-line cards
 * carried — so the Overview keeps its at-a-glance signal without the full
 * inventory table.
 */

interface Props {
  summary?: FleetSummary;
  isLoading: boolean;
  samples: FleetSample[];
}

export function FleetKpiStrip({ summary, isLoading, samples }: Props) {
  if (isLoading || !summary) {
    return (
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="card">
            <div className="skeleton h-4 w-20 rounded" />
            <div className="skeleton mt-3 h-8 w-24 rounded" />
            <div className="skeleton mt-3 h-8 w-full rounded" />
          </div>
        ))}
      </div>
    );
  }

  const totalHosts = summary.host_count ?? Object.values(summary.counts ?? {}).reduce((a, b) => a + b, 0);
  const healthy = summary.healthy_count ?? summary.counts?.healthy ?? 0;
  const notHealthy = totalHosts - healthy;

  const diskPct = summary.total_disk_bytes > 0 ? (summary.total_disk_used / summary.total_disk_bytes) * 100 : 0;

  // Live fleet mean from the sample ring (falls back to the backend mean until
  // the first sample arrives, so the tile is never empty).
  const lastSample = samples.length > 0 ? samples[samples.length - 1] : null;
  const cpuNow = lastSample?.cpu ?? summary.mean_cpu_pct ?? 0;
  const memNow = lastSample?.mem ?? summary.mean_mem_pct ?? 0;
  const rxNow = lastSample?.rx ?? 0;
  const txNow = lastSample?.tx ?? 0;

  const cpuSeries = samples.map((s) => s.cpu);
  const memSeries = samples.map((s) => s.mem);
  const netSeries = samples.map((s) => s.rx);

  const tiles: Tile[] = [
    {
      label: "Hosts",
      value: `${totalHosts}`,
      accent: "text-status-healthy",
      sub: `${healthy} healthy · ${notHealthy} not`,
      subColor: notHealthy > 0 ? "text-status-degraded" : "text-ink-muted",
      extra: <StatusBars counts={summary.counts ?? {}} />,
    },
    {
      label: "Mean CPU",
      value: percent(cpuNow, 1),
      accent: thresholdColor(cpuNow),
      sub: `${summary.containers_running} containers running`,
      subColor: "text-ink-muted",
      spark: cpuSeries,
      sparkColor: "var(--accent)",
    },
    {
      label: "Memory",
      value: percent(memNow, 1),
      accent: thresholdColor(memNow),
      sub: summary.total_mem_bytes > 0 ? `${bytes(summary.total_mem_used)} / ${bytes(summary.total_mem_bytes)}` : "—",
      subColor: "text-ink-muted",
      spark: memSeries,
      sparkColor: "var(--status-degraded)",
    },
    {
      label: "Disk",
      value: percent(diskPct, 1),
      accent: thresholdColor(diskPct),
      sub: summary.total_disk_bytes > 0 ? `${bytes(summary.total_disk_used)} / ${bytes(summary.total_disk_bytes)}` : "—",
      subColor: "text-ink-muted",
      extra: notHealthy > 0 ? undefined : <NetHint rx={rxNow} tx={txNow} spark={netSeries} />,
    },
  ];

  return (
    <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
      {tiles.map((t) => (
        <div key={t.label} className="card">
          <div className="metric-label">{t.label}</div>
          <div className={`mt-1 text-2xl font-bold tracking-tight tabular-nums ${t.accent}`}>{t.value}</div>
          <div className={`mt-0.5 text-xs ${t.subColor}`}>{t.sub}</div>
          {t.spark && <div className="mt-2"><Sparkline data={t.spark} color={t.sparkColor} width={240} height={28} /></div>}
          {t.extra}
        </div>
      ))}
    </div>
  );
}

interface Tile {
  label: string;
  value: string;
  accent: string;
  sub: string;
  subColor: string;
  spark?: (number | null)[];
  sparkColor?: string;
  extra?: React.ReactNode;
}

function thresholdColor(v: number): string {
  if (v > 90) return "text-danger";
  if (v > 75) return "text-status-degraded";
  if (v > 50) return "text-status-stale";
  return "text-status-healthy";
}

function NetHint({ rx, tx, spark }: { rx: number; tx: number; spark: (number | null)[] }) {
  return (
    <div className="mt-2">
      <div className="mb-1 flex justify-between text-xs text-ink-muted">
        <span className="text-status-healthy">↓ {rate(rx)}</span>
        <span className="text-status-degraded">↑ {rate(tx)}</span>
      </div>
      <Sparkline data={spark} color="var(--status-healthy)" width={240} height={20} />
    </div>
  );
}

function StatusBars({ counts }: { counts: Record<string, number> }) {
  const colors: Record<string, string> = {
    healthy: "bg-status-healthy",
    degraded: "bg-status-degraded",
    stale: "bg-status-stale",
    no_agent: "bg-status-noagent",
    "no-agent": "bg-status-noagent",
    offline: "bg-status-offline",
  };
  const bars: React.ReactNode[] = [];
  for (const [status, count] of Object.entries(counts)) {
    const color = colors[status] || "bg-status-offline";
    for (let i = 0; i < count; i++) {
      bars.push(<div key={`${status}-${i}`} className={`h-1.5 w-1.5 rounded-full ${color}`} />);
    }
  }
  return <div className="mt-2 flex flex-wrap gap-0.5">{bars}</div>;
}
