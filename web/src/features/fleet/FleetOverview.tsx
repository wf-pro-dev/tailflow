import { useState } from "react";
import { useFleetSummary, useHosts } from "@/lib/api/hooks";
import { useStreamStatus } from "@/lib/stream/statusStore";
import { HostTable } from "./HostTable";
import { FleetSummaryCards } from "./FleetSummaryCards";
import { FleetMetrics } from "./FleetMetrics";
import { ResourceDistribution } from "./ResourceDistribution";
import { Filters } from "./Filters";
import { StreamBanner } from "@/components/StreamBanner";

interface Filters {
  q?: string;
  status?: string;
  sort?: string;
  order?: "asc" | "desc";
}

export function FleetOverview() {
  const [filters, setFilters] = useState<Filters>({ sort: "name", order: "asc" });
  const hostsQuery = useHosts(filters);
  const summaryQuery = useFleetSummary();

  // Stream status comes from the shared store (root owns the base subscription).
  const status = useStreamStatus();

  const onSort = (sort: string, order: "asc" | "desc") => setFilters((f) => ({ ...f, sort, order }));

  return (
    <div className="space-y-4">
      <StreamBanner status={status} />

      {/* Bento row 1 — KPI strip */}
      <FleetSummaryCards summary={summaryQuery.data} isLoading={summaryQuery.isLoading} />

      {/* Bento row 2 — fleet charts (wide) + resource distribution (sidebar) */}
      <div className="grid gap-4 lg:grid-cols-4">
        <div className="lg:col-span-3">
          <FleetMetrics />
        </div>
        <div className="lg:col-span-1">
          <ResourceDistribution summary={summaryQuery.data} />
        </div>
      </div>

      {/* Bento row 3 — host table (full width, dominant) */}
      <div className="card !p-0">
        <Filters filters={filters} onChange={setFilters} />
        <HostTable
          hosts={hostsQuery.data ?? []}
          isLoading={hostsQuery.isLoading}
          error={hostsQuery.error}
          sort={filters.sort}
          order={filters.order}
          onSort={onSort}
        />
      </div>
    </div>
  );
}
