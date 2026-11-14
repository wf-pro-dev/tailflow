import { useState, useRef, useEffect, useCallback, memo } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import type { Host } from "@/lib/api/client";
import { useHostMetrics, useHostMetricsLatest } from "@/lib/api/hooks";
import { StatusBadge } from "./StatusBadge";
import { useDensity } from "@/features/ui/DensityProvider";
import { relativeTime, percent } from "@/lib/format";

/*
 * HostTable — the mature host inventory. No per-row sparklines (trends belong
 * on the host detail page; Datadog's Host List uses metric columns, not row
 * sparklines). Each metric is a clean threshold-coloured value, stream-driven
 * for CPU/Memory (the metrics series is patched by the live sample) and from
 * the latest snapshot for Disk. Row click drills into host detail; j/k/Enter
 * keyboard navigation is retained. The expandable inline panel is removed in
 * favour of the dedicated detail page, which can show far more.
 */

interface Props {
  hosts: Host[];
  isLoading: boolean;
  error: Error | null;
  sort?: string;
  order?: "asc" | "desc";
  onSort?: (sort: string, order: "asc" | "desc") => void;
}

type Sortable = "name" | "status";

export function HostTable({ hosts, isLoading, error, sort = "name", order = "asc", onSort }: Props) {
  const { rowPad, cellPad } = useDensity();
  const navigate = useNavigate();
  const [selIdx, setSelIdx] = useState(-1);
  const tableRef = useRef<HTMLDivElement>(null);

  const toggleSort = useCallback(
    (col: Sortable) => {
      if (!onSort) return;
      if (sort === col) onSort(col, order === "asc" ? "desc" : "asc");
      else onSort(col, "asc");
    },
    [sort, order, onSort],
  );

  // j/k/enter keyboard navigation.
  useEffect(() => {
    const el = tableRef.current;
    if (!el) return;
    const onKey = (e: KeyboardEvent) => {
      if (hosts.length === 0) return;
      if (e.key === "j" || e.key === "ArrowDown") {
        e.preventDefault();
        setSelIdx((i) => Math.min(i + 1, hosts.length - 1));
      } else if (e.key === "k" || e.key === "ArrowUp") {
        e.preventDefault();
        setSelIdx((i) => Math.max(i - 1, 0));
      } else if (e.key === "Enter" && selIdx >= 0) {
        e.preventDefault();
        navigate({ to: "/hosts/$host/metrics", params: { host: hosts[selIdx].name } });
      }
    };
    el.addEventListener("keydown", onKey);
    return () => el.removeEventListener("keydown", onKey);
  }, [hosts, selIdx, navigate]);

  if (isLoading) {
    return (
      <div className="space-y-1 p-2">
        {[0, 1, 2, 3, 4].map((i) => (
          <div key={i} className="skeleton h-12 rounded-lg" />
        ))}
      </div>
    );
  }

  if (error) {
    return (
      <div className="py-12 text-center text-danger">
        Error loading hosts: {error.message}
      </div>
    );
  }

  if (hosts.length === 0) {
    return (
      <div className="py-16 text-center text-ink-muted">
        <svg className="mx-auto mb-3 opacity-30" width="48" height="48" viewBox="0 0 48 48" fill="none">
          <rect x="6" y="10" width="36" height="10" rx="2" stroke="currentColor" strokeWidth="2" />
          <rect x="6" y="28" width="36" height="10" rx="2" stroke="currentColor" strokeWidth="2" />
          <circle cx="12" cy="15" r="1.5" fill="currentColor" />
          <circle cx="12" cy="33" r="1.5" fill="currentColor" />
        </svg>
        <p className="text-sm font-medium text-ink-secondary">No hosts discovered</p>
        <p className="mt-1 text-xs">
          The fleet is empty or the collector hasn't started yet. If you just
          added a node, wait a few seconds for the first sample.
        </p>
      </div>
    );
  }

  const SortArrow = ({ col }: { col: Sortable }) =>
    sort === col ? (
      <span className="ml-1 text-ink-muted">{order === "asc" ? "↑" : "↓"}</span>
    ) : (
      <span className="ml-1 text-ink-faint opacity-0 group-hover:opacity-100">↕</span>
    );

  return (
    <div className="overflow-x-auto" ref={tableRef} tabIndex={0}>
      <table className="w-full text-sm">
        <thead className="sticky top-0 z-10 bg-surface">
          <tr className="group border-b border-line text-left text-[11px] font-medium uppercase tracking-[0.06em] text-ink-muted">
            <th className={`${cellPad} py-2 font-medium`}>Status</th>
            <th
              className={`${cellPad} py-2 font-medium ${onSort ? "cursor-pointer select-none hover:text-ink-secondary" : ""}`}
              onClick={() => toggleSort("name")}
            >
              Host <SortArrow col="name" />
            </th>
            <th className={`${cellPad} py-2 text-right font-medium`}>CPU</th>
            <th className={`${cellPad} py-2 text-right font-medium`}>Memory</th>
            <th className={`${cellPad} py-2 text-right font-medium`}>Disk</th>
            <th className={`${cellPad} py-2 font-medium`}>Environment</th>
            <th className={`${cellPad} py-2 font-medium`}>Tags</th>
            <th
              className={`${cellPad} py-2 font-medium ${onSort ? "cursor-pointer select-none hover:text-ink-secondary" : ""}`}
              onClick={() => toggleSort("status")}
            >
              Last sample <SortArrow col="status" />
            </th>
          </tr>
        </thead>
        <tbody>
          {hosts.map((h, i) => (
            <HostRow key={h.name} host={h} selected={i === selIdx} rowPad={rowPad} cellPad={cellPad} />
          ))}
        </tbody>
      </table>
      <div className="border-t border-line-subtle px-4 py-2 text-xs text-ink-muted">
        {hosts.length} {hosts.length === 1 ? "host" : "hosts"}
        <span className="ml-3 text-ink-faint">
          <kbd className="rounded border border-line px-1">j</kbd>
          <kbd className="ml-0.5 rounded border border-line px-1">k</kbd> navigate ·
          <kbd className="ml-1 rounded border border-line px-1">↵</kbd> open
        </span>
      </div>
    </div>
  );
}

function lastVal(arr: (number | null)[]): number | null {
  for (let i = arr.length - 1; i >= 0; i--) {
    if (arr[i] != null && !isNaN(arr[i] as number)) return arr[i] as number;
  }
  return null;
}

function valColor(v: number | null): string {
  if (v == null) return "text-ink-muted";
  if (v > 90) return "text-danger";
  if (v > 75) return "text-status-degraded";
  if (v > 50) return "text-status-stale";
  return "text-ink-primary";
}

interface RowProps {
  host: Host;
  selected: boolean;
  rowPad: string;
  cellPad: string;
}

const HostRow = memo(function HostRow({ host, selected, rowPad, cellPad }: RowProps) {
  const navigate = useNavigate();
  // CPU/Memory are stream-driven (the series is patched by the live sample);
  // Disk comes from the latest snapshot.
  const { data } = useHostMetrics(host.name, "15m");
  const { data: latest } = useHostMetricsLatest(host.name);

  const cpu = lastVal(data?.series.cpu_total ?? []);
  const mem = lastVal(data?.series.mem_used_pct ?? []);
  const disks = latest?.disks ?? [];
  const disk = disks.length > 0 ? Math.max(...disks.map((d) => d.used_pct)) : null;

  const open = () => navigate({ to: "/hosts/$host/metrics", params: { host: host.name } });

  return (
    <tr
      className={`table-row group cursor-pointer border-l-2 ${
        selected ? "border-l-accent bg-surface-raised" : "border-l-transparent"
      }`}
      onClick={open}
    >
      <td className={`${cellPad} ${rowPad}`}>
        <StatusBadge status={host.status} />
      </td>
      <td className={`${cellPad} ${rowPad}`}>
        <Link
          to="/hosts/$host/metrics"
          params={{ host: host.name }}
          onClick={(e) => e.stopPropagation()}
          className="font-medium text-accent hover:text-accent-hover transition-colors"
        >
          {host.name}
        </Link>
      </td>
      <td className={`${cellPad} ${rowPad} text-right tabular-nums ${valColor(cpu)}`}>{percent(cpu, 0)}</td>
      <td className={`${cellPad} ${rowPad} text-right tabular-nums ${valColor(mem)}`}>{percent(mem, 0)}</td>
      <td className={`${cellPad} ${rowPad} text-right tabular-nums ${valColor(disk)}`}>{percent(disk, 0)}</td>
      <td className={`${cellPad} ${rowPad} text-ink-secondary`}>{host.environment || "—"}</td>
      <td className={`${cellPad} ${rowPad}`}>
        <div className="flex flex-wrap gap-1">
          {(host.tags ?? []).slice(0, 3).map((t) => (
            <span key={t} className="rounded bg-surface-raised px-1.5 py-0.5 text-xs text-ink-secondary">
              {t}
            </span>
          ))}
          {(host.tags ?? []).length > 3 && (
            <span className="px-1 text-xs text-ink-faint">+{(host.tags ?? []).length - 3}</span>
          )}
        </div>
      </td>
      <td className={`${cellPad} ${rowPad} text-ink-muted`}>{relativeTime(host.last_sample_at)}</td>
    </tr>
  );
});
