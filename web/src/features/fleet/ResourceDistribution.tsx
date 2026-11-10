import type { FleetSummary, HostResource, ContainerResource } from "@/lib/api/client";
import { percent, bytes } from "@/lib/format";

interface Props {
  summary?: FleetSummary;
}

export function ResourceDistribution({ summary }: Props) {
  if (!summary) return null;

  const hasHostData =
    (summary.top_cpu_hosts?.length ?? 0) > 0 ||
    (summary.top_mem_hosts?.length ?? 0) > 0 ||
    (summary.top_disk_hosts?.length ?? 0) > 0;
  const hasContainerData = (summary.top_containers?.length ?? 0) > 0;

  if (!hasHostData && !hasContainerData) return null;

  return (
    <div className="space-y-4">
      {hasHostData && (
        <div className="card">
          <h3 className="mb-4 text-sm font-medium text-zinc-300">Resource Distribution by Host</h3>
          <p className="mb-3 text-xs text-zinc-500">% of total fleet usage</p>
          <div className="space-y-5">
            <ResourceSection
              title="CPU"
              hosts={summary.top_cpu_hosts}
              color="bg-blue-500"
            />
            <ResourceSection
              title="Memory"
              hosts={summary.top_mem_hosts}
              color="bg-purple-500"
            />
            <ResourceSection
              title="Disk"
              hosts={summary.top_disk_hosts}
              color="bg-orange-500"
            />
          </div>
        </div>
      )}

      {hasContainerData && (
        <div className="card">
          <h3 className="mb-4 text-sm font-medium text-zinc-300">Top Containers by CPU</h3>
          <div className="space-y-2">
            {summary.top_containers.map((c) => (
              <ContainerRow key={`${c.host}:${c.container_id}`} container={c} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function ResourceSection({
  title,
  hosts,
  color,
}: {
  title: string;
  hosts: HostResource[];
  color: string;
}) {
  if (!hosts || hosts.length === 0) return null;

  return (
    <div>
      <div className="metric-label mb-2">{title}</div>
      <div className="space-y-2">
        {hosts.map((h) => (
          <div key={h.name} className="flex items-center gap-3 text-xs">
            <span className="w-20 truncate text-zinc-400">{h.name}</span>
            <div className="flex-1 overflow-hidden rounded-full bg-white/[0.04]">
              <div
                className={`h-2 rounded-full ${color} transition-all duration-300`}
                style={{ width: `${Math.max(h.fleet_share ?? 0, 2)}%` }}
              />
            </div>
            <span className="w-16 text-right font-mono text-zinc-500">
              {percent(h.fleet_share ?? 0, 1)}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

function ContainerRow({ container }: { container: ContainerResource }) {
  const maxCPU = 100; // Container CPU % can exceed 100 on multi-core, but we cap the bar
  return (
    <div className="flex items-center gap-3 text-xs">
      <div className="flex w-32 flex-col truncate">
        <span className="truncate text-zinc-300">{container.name}</span>
        <span className="truncate text-zinc-600">{container.host}</span>
      </div>
      <div className="flex-1 overflow-hidden rounded-full bg-white/[0.04]">
        <div
          className="h-2 rounded-full bg-cyan-500 transition-all duration-300"
          style={{ width: `${Math.min((container.cpu_percent / maxCPU) * 100, 100)}%` }}
        />
      </div>
      <span className="w-12 text-right font-mono text-zinc-500">
        {percent(container.cpu_percent, 1)}
      </span>
      <span className="w-16 text-right font-mono text-zinc-600">
        {bytes(container.mem_used_bytes)}
      </span>
    </div>
  );
}
