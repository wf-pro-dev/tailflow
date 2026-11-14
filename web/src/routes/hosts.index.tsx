import { useState } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { useHosts } from "@/lib/api/hooks";
import { HostTable } from "@/features/fleet/HostTable";
import { Filters } from "@/features/fleet/Filters";

/*
 * Hosts — the dedicated host inventory (formerly a card grid that duplicated
 * the Overview's host table). Now a single mature table with filters, sort and
 * keyboard navigation. The card grid and its duplicate StreamBanner are gone;
 * the StreamBanner is a single global element in the root layout.
 */

interface FiltersState {
  q?: string;
  status?: string;
  sort?: string;
  order?: "asc" | "desc";
}

export const Route = createFileRoute("/hosts/")({
  component: HostsPage,
});

function HostsPage() {
  const [filters, setFilters] = useState<FiltersState>({ sort: "name", order: "asc" });
  const hostsQuery = useHosts(filters);
  const onSort = (sort: string, order: "asc" | "desc") => setFilters((f) => ({ ...f, sort, order }));

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-bold tracking-tight">Hosts</h1>
        <span className="text-sm text-ink-muted">
          {hostsQuery.data?.length ?? 0} {(hostsQuery.data?.length ?? 0) === 1 ? "host" : "hosts"}
        </span>
      </div>

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
