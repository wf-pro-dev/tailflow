interface Props {
  filters: { q?: string; status?: string; sort?: string };
  onChange: (f: { q?: string; status?: string; sort?: string }) => void;
}

export function Filters({ filters, onChange }: Props) {
  return (
    <div className="mb-4 flex flex-wrap gap-2">
      <input
        type="text"
        placeholder="Search hosts…"
        value={filters.q ?? ""}
        onChange={(e) => onChange({ ...filters, q: e.target.value })}
        className="input min-w-[200px] flex-1"
      />
      <select
        value={filters.status ?? ""}
        onChange={(e) => onChange({ ...filters, status: e.target.value || undefined })}
        className="input"
      >
        <option value="">All statuses</option>
        <option value="healthy">Healthy</option>
        <option value="degraded">Degraded</option>
        <option value="stale">Stale</option>
        <option value="no-agent">No agent</option>
        <option value="offline">Offline</option>
      </select>
      <select
        value={filters.sort ?? "name"}
        onChange={(e) => onChange({ ...filters, sort: e.target.value })}
        className="input"
      >
        <option value="name">Sort: Name</option>
        <option value="status">Sort: Status</option>
      </select>
    </div>
  );
}
