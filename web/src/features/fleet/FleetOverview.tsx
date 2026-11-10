import { useState, useMemo } from "react";
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
      <FleetSummaryCards summary={summaryQuery.data} isLoading={summaryQuery.isLoading} />

      {/* Fleet metrics + resource distribution side by side */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <div className="lg:col-span-2">
          <FleetMetrics />
        </div>
        <div>
          <ResourceDistribution summary={summaryQuery.data} />
        </div>
      </div>

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
