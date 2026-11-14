import { Link } from "@tanstack/react-router";
import type { FleetSummary, HostResource } from "@/lib/api/client";
import { percent } from "@/lib/format";

/*
 * TopProblemHosts — a short ranked list of the hosts under the most CPU load,
 * pulled from the fleet summary's top_cpu_hosts (which the backend already
 * ranks). Each row links straight into the host detail page. This is the
 * Overview's "where should I look first" drill-down, deliberately capped so
 * the full inventory (which used to live here as a table) lives on the
 * dedicated Hosts screen instead.
 */

interface Props {
  summary?: FleetSummary;
}

export function TopProblemHosts({ summary }: Props) {
  const hosts: HostResource[] = summary?.top_cpu_hosts ?? [];

  if (hosts.length === 0) {
    return (
      <div className="card py-10 text-center text-sm text-ink-muted">
        No resource data yet.
      </div>
    );
  }

  const top = hosts.slice(0, 6);
  const maxCpu = Math.max(...top.map((h) => h.cpu_pct ?? 0), 1);

  return (
    <div className="card">
      <div className="mb-3 flex items-center justify-between">
        <h3 className="text-sm font-medium text-ink-secondary">Top Hosts by CPU</h3>
        <Link to="/hosts" className="text-xs text-accent hover:text-accent-hover">
          All hosts →
        </Link>
      </div>
      <div className="space-y-2">
        {top.map((h, i) => {
          const cpu = h.cpu_pct ?? 0;
          const barColor =
            cpu > 90 ? "bg-danger" : cpu > 75 ? "bg-status-degraded" : cpu > 50 ? "bg-status-stale" : "bg-status-healthy";
          return (
            <Link
              key={h.name}
              to="/hosts/$host/metrics"
              params={{ host: h.name }}
              className="flex items-center gap-3 rounded-lg px-2 py-1.5 transition-colors hover:bg-surface-raised"
            >
              <span className="w-4 text-right text-xs tabular-nums text-ink-faint">{i + 1}</span>
              <span className="w-32 truncate font-medium text-ink-secondary">{h.name}</span>
              <div className="flex-1 overflow-hidden rounded-full bg-sunken">
                <div
                  className={`h-1.5 rounded-full ${barColor} transition-all duration-300`}
                  style={{ width: `${Math.max((cpu / maxCpu) * 100, 3)}%` }}
                />
              </div>
              <span className="w-14 text-right text-xs tabular-nums text-ink-muted">{percent(cpu, 1)}</span>
            </Link>
          );
        })}
      </div>
    </div>
  );
}
