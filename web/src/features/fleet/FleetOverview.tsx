import { useState } from "react";
import { useFleetSummary, useHosts } from "@/lib/api/hooks";
import { useEventStream } from "@/lib/stream/useEventStream";
import { HostTable } from "./HostTable";
import { FleetSummaryCards } from "./FleetSummaryCards";
import { Filters } from "./Filters";

export function FleetOverview() {
  const [filters, setFilters] = useState<{ q?: string; status?: string; sort?: string }>({});
  const hostsQuery = useHosts(filters);
  const summaryQuery = useFleetSummary();

  // Subscribe to host.updated and host.removed events.
  useEventStream(["hosts"]);

  return (
    <div className="space-y-6">
      <FleetSummaryCards summary={summaryQuery.data} isLoading={summaryQuery.isLoading} />
      <div className="card">
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
