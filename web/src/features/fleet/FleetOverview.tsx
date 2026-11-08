import { useState } from "react";
import { useFleetSummary, useHosts } from "@/lib/api/hooks";
import { useEventStream } from "@/lib/stream/useEventStream";
import { HostTable } from "./HostTable";
import { FleetSummaryCards } from "./FleetSummaryCards";
import { FleetMetrics } from "./FleetMetrics";
import { Filters } from "./Filters";
import { StreamBanner } from "@/components/StreamBanner";

export function FleetOverview() {
  const [filters, setFilters] = useState<{ q?: string; status?: string; sort?: string }>({});
  const hostsQuery = useHosts(filters);
  const summaryQuery = useFleetSummary();

  // Subscribe to host.updated and host.removed events.
  const { status } = useEventStream(["hosts"]);

  return (
    <div className="space-y-6">
      <StreamBanner status={status} />
      <FleetSummaryCards summary={summaryQuery.data} isLoading={summaryQuery.isLoading} />

      {/* Fleet-level live metrics charts */}
      <FleetMetrics />

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
