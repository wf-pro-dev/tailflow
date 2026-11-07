import { bytes, percent } from "@/lib/format";

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
          <div key={i} className="card animate-pulse h-24" />
        ))}
      </div>
    );
  }

  const cards = [
    {
      label: "Hosts",
      value: `${summary.counts.healthy ?? 0} healthy`,
      sub: `${(summary.counts.degraded ?? 0) + (summary.counts.stale ?? 0) + (summary.counts.no_agent ?? 0) + (summary.counts.offline ?? 0)} not healthy`,
      color: "text-green-600",
    },
    {
      label: "Mean CPU",
      value: percent(summary.mean_cpu_pct, 1),
      sub: "fleet-wide",
      color: "text-blue-600",
    },
    {
      label: "Mean Memory",
      value: percent(summary.mean_mem_pct, 1),
      sub: "fleet-wide",
      color: "text-purple-600",
    },
    {
      label: "Containers",
      value: `${summary.containers_running}`,
      sub: "running",
      color: "text-orange-600",
    },
  ];

  return (
    <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
      {cards.map((c) => (
        <div key={c.label} className="card">
          <div className="text-xs font-medium uppercase text-gray-500 dark:text-gray-400">
            {c.label}
          </div>
          <div className={`mt-1 text-2xl font-bold ${c.color}`}>{c.value}</div>
          <div className="mt-1 text-xs text-gray-500 dark:text-gray-400">{c.sub}</div>
        </div>
      ))}
    </div>
  );
}
