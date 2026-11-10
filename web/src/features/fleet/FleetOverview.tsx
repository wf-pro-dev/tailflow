import { useState } from "react";
import { useFleetSummary, useHosts } from "@/lib/api/hooks";
import { useEventStream } from "@/lib/stream/useEventStream";
import { HostTable } from "./HostTable";
import { FleetSummaryCards } from "./FleetSummaryCards";
import { FleetMetrics } from "./FleetMetrics";
import { ResourceDistribution } from "./ResourceDistribution";
import { Filters } from "./Filters";
import { StreamBanner } from "@/components/StreamBanner";

export function FleetOverview() {
  const [filters, setFilters] = useState<{ q?: string; status?: string; sort?: string }>({});
  const hostsQuery = useHosts(filters);
  const summaryQuery = useFleetSummary();

  // Subscribe to host events + fleet summary events
  const { status } = useEventStream(["hosts", "fleet"]);

  return (
    <div className="space-y-6">
      <StreamBanner status={status} />

      {/* Row 1: KPI cards (Hosts, Memory, Disk) */}
      <FleetSummaryCards summary={summaryQuery.data} isLoading={summaryQuery.isLoading} />

      {/* Row 2: Fleet metrics charts (full width, 3 columns) */}
      <FleetMetrics />

      {/* Row 3: Resource distribution + top containers */}
      <ResourceDistribution summary={summaryQuery.data} />

      {/* Row 4: Host table */}
      <div className="card !p-0">
        <Filters filters={filters} onChange={setFilters} />
        <HostTable
          hosts={hostsQuery.data ?? []}
          isLoading={hostsQuery.isLoading}
          error={hostsQuery.error}
        />
      </div>
    </div>
  );
}
