import { percent } from "@/lib/format";

interface Props {
  summary?: {
    counts: Record<string, number>;
    mean_cpu_pct: number | null;
    mean_mem_pct: number | null;
    containers_running: number;
  };
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

  const totalHosts = Object.values(summary.counts).reduce((a, b) => a + b, 0);
  const healthy = summary.counts.healthy ?? 0;
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
      sparkline: summary.mean_cpu_pct !== null,
    },
    {
      label: "Mean Memory",
      value: percent(summary.mean_mem_pct, 1),
      sub: "fleet-wide",
      subColor: "text-zinc-500",
      accent: "text-purple-400",
      sparkline: summary.mean_mem_pct !== null,
    },
    {
      label: "Containers",
      value: `${summary.containers_running}`,
      sub: "running",
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
            <div className="mt-3 flex gap-0.5">
              {c.bars}
            </div>
          )}
          {c.sparkline && (
            <div className="mt-3 flex h-8 items-end gap-px">
              {Array.from({ length: 20 }).map((_, i) => (
                <div
                  key={i}
                  className="flex-1 rounded-sm bg-current opacity-20"
                  style={{
                    height: `${20 + Math.sin(i * 0.5) * 30 + Math.random() * 30}%`,
                    minHeight: "2px",
                  }}
                />
              ))}
            </div>
          )}
        </div>
      ))}
    </div>
  );
}

// Generate colored status bars for the hosts card
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
