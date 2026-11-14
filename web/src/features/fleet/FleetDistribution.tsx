import { Link } from "@tanstack/react-router";
import type { Host } from "@/lib/api/client";
import { useChartColors } from "@/lib/chartColors";
import type { HostLatest } from "./useFleetSamples";

/*
 * FleetDistribution — a color-encoded host grid: one cell per host, coloured by
 * current CPU (green → amber → red) or by host status when there are no
 * samples. Hosts are grouped by environment so a clump of red cells in one
 * environment is visible at a glance — the triage signal the Overview exists
 * to surface (replacing the removed ResourceDistribution progress bars, which
 * self-hid on small fleets and only ever showed the top few hosts).
 *
 * Custom SVG/div grid rather than a chart primitive: Recharts has no native
 * heatmap, and a div grid is cheaper and crisper at fleet scale.
 */

interface Props {
  hosts: Host[];
  perHost: Record<string, HostLatest>;
}

export function FleetDistribution({ hosts, perHost }: Props) {
  const colors = useChartColors();

  if (hosts.length === 0) {
    return (
      <div className="card py-10 text-center text-sm text-ink-muted">
        No hosts to distribute.
      </div>
    );
  }

  // Group by environment (ungrouped hosts land in "—").
  const groups = new Map<string, Host[]>();
  for (const h of hosts) {
    const env = h.environment || "—";
    const arr = groups.get(env) ?? [];
    arr.push(h);
    groups.set(env, arr);
  }

  const cellColor = (h: Host): string => {
    // No metric sample → colour by host status.
    if (h.status === "offline" || h.status === "no-agent") return colors.offline;
    if (h.status === "stale") return colors.stale;
    const latest = perHost[h.name];
    if (!latest) return colors.grid; // awaiting first sample
    const cpu = latest.cpu;
    if (cpu > 90) return colors.danger;
    if (cpu > 75) return colors.degraded;
    if (cpu > 50) return "#E8A33D"; // amber
    return colors.healthy;
  };

  return (
    <div className="card">
      <div className="mb-3 flex items-center justify-between">
        <h3 className="text-sm font-medium text-ink-secondary">Fleet Distribution</h3>
        <Legend colors={colors} />
      </div>
      <div className="space-y-4">
        {[...groups.entries()].map(([env, group]) => (
          <div key={env}>
            <div className="metric-label mb-2">{env}</div>
            <div className="flex flex-wrap gap-1">
              {group.map((h) => {
                const latest = perHost[h.name];
                const cpu = latest?.cpu;
                const title = latest
                  ? `${h.name} · CPU ${cpu != null ? cpu.toFixed(0) : "?"}% · ${h.status}`
                  : `${h.name} · ${h.status}`;
                return (
                  <Link
                    key={h.name}
                    to="/hosts/$host/metrics"
                    params={{ host: h.name }}
                    title={title}
                    className="h-7 w-7 rounded transition-transform hover:scale-110 hover:ring-2 hover:ring-accent/60"
                    style={{ backgroundColor: cellColor(h) }}
                  />
                );
              })}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function Legend({ colors }: { colors: ReturnType<typeof useChartColors> }) {
  const items: [string, string][] = [
    ["ok", colors.healthy],
    ["busy", "#E8A33D"],
    ["high", colors.degraded],
    ["crit", colors.danger],
    ["stale", colors.stale],
    ["down", colors.offline],
  ];
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[10px] text-ink-muted">
      {items.map(([label, color]) => (
        <span key={label} className="flex items-center gap-1">
          <span className="h-2.5 w-2.5 rounded" style={{ backgroundColor: color }} />
          {label}
        </span>
      ))}
    </div>
  );
}
