import { useFleetSummary, useHosts } from "@/lib/api/hooks";
import { FleetKpiStrip } from "./FleetKpiStrip";
import { FleetDistribution } from "./FleetDistribution";
import { TopProblemHosts } from "./TopProblemHosts";
import { useFleetSamples } from "./useFleetSamples";

/*
 * FleetOverview — the redesigned Overview: a pure fleet-health summary with no
 * full host table. The hero KPI strip (stream-driven sparklines) answers "is
 * the fleet OK"; the distribution grid reveals where problems cluster; the
 * top-problem-hosts list drills into the worst few. The full inventory now
 * lives on the dedicated Hosts screen.
 *
 * StreamBanner is no longer rendered here — it is a single global element in
 * the root layout.
 */
export function FleetOverview() {
  const hostsQuery = useHosts();
  const summaryQuery = useFleetSummary();
  const hosts = hostsQuery.data ?? [];

  // One fleet-wide metrics subscription drives both the KPI sparklines (ring)
  // and the distribution grid (per-host latest).
  const { ring, perHost } = useFleetSamples(hosts.map((h) => h.name));

  return (
    <div className="space-y-4">
      {/* Hero — fleet KPI strip with stream-driven sparklines */}
      <FleetKpiStrip summary={summaryQuery.data} isLoading={summaryQuery.isLoading} samples={ring} />

      {/* Distribution grid (wide) + top-problem hosts (sidebar) */}
      <div className="grid gap-4 lg:grid-cols-3">
        <div className="lg:col-span-2">
          <FleetDistribution hosts={hosts} perHost={perHost} />
        </div>
        <div className="lg:col-span-1">
          <TopProblemHosts summary={summaryQuery.data} />
        </div>
      </div>
    </div>
  );
}
