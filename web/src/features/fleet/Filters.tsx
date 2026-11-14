interface Filters {
  q?: string;
  status?: string;
  sort?: string;
  order?: "asc" | "desc";
}

interface Props {
  filters: Filters;
  onChange: (f: Filters) => void;
}

export function Filters({ filters, onChange }: Props) {
  return (
    <div className="flex flex-wrap items-center gap-2 border-b border-line-subtle p-4">
      <div className="relative min-w-[200px] flex-1">
        <svg
          className="absolute left-3 top-1/2 -translate-y-1/2 text-ink-muted"
          width="14" height="14" viewBox="0 0 14 14" fill="none"
        >
          <circle cx="6" cy="6" r="4.5" stroke="currentColor" strokeWidth="1.5" />
          <path d="M9.5 9.5L12 12" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
        </svg>
        <input
          type="text"
          placeholder="Search hosts…"
          value={filters.q ?? ""}
          onChange={(e) => onChange({ ...filters, q: e.target.value })}
          className="input w-full pl-9"
        />
      </div>
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
      <span className="ml-auto text-xs text-ink-faint">
        Sort by clicking a column header
      </span>
    </div>
  );
}
