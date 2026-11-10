import type { FleetSummary, HostResource } from "@/lib/api/client";
import { percent } from "@/lib/format";

interface Props {
  summary?: FleetSummary;
}

export function ResourceDistribution({ summary }: Props) {
  if (!summary) return null;

  const hasData =
    (summary.top_cpu_hosts?.length ?? 0) > 0 ||
    (summary.top_mem_hosts?.length ?? 0) > 0 ||
    (summary.top_disk_hosts?.length ?? 0) > 0;

  if (!hasData) return null;

  return (
    <div className="card">
      <h3 className="mb-4 text-sm font-medium text-zinc-300">Resource Distribution</h3>
      <div className="space-y-5">
        <ResourceSection
          title="CPU"
          hosts={summary.top_cpu_hosts}
          getValue={(h) => h.cpu_pct}
          color="bg-blue-500"
        />
        <ResourceSection
          title="Memory"
          hosts={summary.top_mem_hosts}
          getValue={(h) => h.mem_pct}
          color="bg-purple-500"
        />
        <ResourceSection
          title="Disk"
          hosts={summary.top_disk_hosts}
          getValue={(h) => h.disk_pct ?? 0}
          color="bg-orange-500"
        />
      </div>
    </div>
  );
}

function ResourceSection({
  title,
  hosts,
  getValue,
  color,
}: {
  title: string;
  hosts: HostResource[];
  getValue: (h: HostResource) => number;
  color: string;
}) {
  if (!hosts || hosts.length === 0) return null;
  const maxVal = Math.max(...hosts.map(getValue), 1);

  return (
    <div>
      <div className="metric-label mb-2">{title}</div>
      <div className="space-y-2">
        {hosts.map((h) => {
          const val = getValue(h);
          const width = (val / maxVal) * 100;
          return (
            <div key={h.name} className="flex items-center gap-3 text-xs">
              <span className="w-20 truncate text-zinc-400">{h.name}</span>
              <div className="flex-1 overflow-hidden rounded-full bg-white/[0.04]">
                <div
                  className={`h-2 rounded-full ${color} transition-all duration-300`}
                  style={{ width: `${Math.max(width, 2)}%` }}
                />
              </div>
              <span className="w-12 text-right font-mono text-zinc-500">
                {percent(val, 1)}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
