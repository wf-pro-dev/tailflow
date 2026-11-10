import { percent, bytes } from "@/lib/format";
import type { FleetSummary } from "@/lib/api/client";

interface Props {
  summary?: FleetSummary;
  isLoading: boolean;
}

export function FleetSummaryCards({ summary, isLoading }: Props) {
  if (isLoading || !summary) {
    return (
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="card">
            <div className="skeleton h-4 w-20 rounded" />
            <div className="skeleton mt-3 h-8 w-24 rounded" />
            <div className="skeleton mt-2 h-3 w-16 rounded" />
          </div>
        ))}
      </div>
    );
  }

  const totalHosts = summary.host_count ?? Object.values(summary.counts).reduce((a, b) => a + b, 0);
  const healthy = summary.healthy_count ?? summary.counts.healthy ?? 0;
  const notHealthy = totalHosts - healthy;

  const cards = [
    {
      label: "Hosts",
      value: `${healthy}`,
      sub: `${notHealthy} not healthy`,
      subColor: notHealthy > 0 ? "text-amber-400" : "text-zinc-500",
      accent: "text-green-400",
      bars: generateStatusBars(summary.counts),
    },
    {
      label: "Mean CPU",
      value: percent(summary.mean_cpu_pct, 1),
      sub: "fleet-wide",
      subColor: "text-zinc-500",
      accent: "text-blue-400",
    },
    {
      label: "Memory",
      value: percent(summary.mean_mem_pct, 1),
      sub: summary.total_mem_bytes > 0
        ? `${bytes(summary.total_mem_used)} / ${bytes(summary.total_mem_bytes)}`
        : "fleet-wide",
      subColor: "text-zinc-500",
      accent: "text-purple-400",
    },
    {
      label: "Disk",
      value: summary.total_disk_bytes > 0
        ? percent((summary.total_disk_used / summary.total_disk_bytes) * 100, 1)
        : "—",
      sub: summary.total_disk_bytes > 0
        ? `${bytes(summary.total_disk_used)} / ${bytes(summary.total_disk_bytes)}`
        : "fleet-wide",
      subColor: "text-zinc-500",
      accent: "text-orange-400",
    },
  ];

  return (
    <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
      {cards.map((c) => (
        <div key={c.label} className="card">
          <div className="metric-label">{c.label}</div>
          <div className={`mt-2 text-3xl font-bold tracking-tight ${c.accent}`}>
            {c.value}
          </div>
          <div className={`mt-1 text-xs ${c.subColor}`}>{c.sub}</div>
          {c.bars && (
            <div className="mt-3 flex flex-wrap gap-0.5">
              {c.bars}
            </div>
          )}
        </div>
      ))}
    </div>
  );
}

function generateStatusBars(counts: Record<string, number>): React.ReactNode {
  const colors: Record<string, string> = {
    healthy: "bg-green-500",
    degraded: "bg-amber-500",
    stale: "bg-lime-500",
    "no_agent": "bg-purple-500",
    "no-agent": "bg-purple-500",
    offline: "bg-zinc-500",
  };

  const bars: React.ReactNode[] = [];
  for (const [status, count] of Object.entries(counts)) {
    const color = colors[status] || "bg-zinc-600";
    for (let i = 0; i < count; i++) {
      bars.push(
        <div
          key={`${status}-${i}`}
          className={`h-1.5 w-1.5 rounded-full ${color}`}
        />
      );
    }
  }
  return bars;
}
