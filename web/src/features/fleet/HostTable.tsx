import { useState, useRef, useEffect, useCallback, memo } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import type { Host } from "@/lib/api/client";
import { useHostMetrics } from "@/lib/api/hooks";
import { StatusBadge } from "./StatusBadge";
import { Sparkline } from "@/components/Sparkline";
import { DiffChip } from "@/components/DiffChip";
import { useDensity } from "@/features/ui/DensityProvider";
import { relativeTime, percent, rate, duration } from "@/lib/format";

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
  const { rowPad, cellPad, sparkH } = useDensity();
  const navigate = useNavigate();
  const [selIdx, setSelIdx] = useState(-1);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const tableRef = useRef<HTMLDivElement>(null);

  const toggleSort = useCallback(
    (col: Sortable) => {
      if (!onSort) return;
      if (sort === col) onSort(col, order === "asc" ? "desc" : "asc");
      else onSort(col, "asc");
    },
    [sort, order, onSort],
  );

  const toggleExpand = useCallback((name: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  }, []);

  // j/k/enter/arrow keyboard navigation.
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
      } else if (e.key === "ArrowRight" && selIdx >= 0) {
        e.preventDefault();
        setExpanded((prev) => new Set(prev).add(hosts[selIdx].name));
      } else if (e.key === "ArrowLeft" && selIdx >= 0) {
        e.preventDefault();
        setExpanded((prev) => {
          const next = new Set(prev);
          next.delete(hosts[selIdx].name);
          return next;
        });
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
            <th className={`${cellPad} py-2 font-medium`}>CPU</th>
            <th className={`${cellPad} py-2 font-medium`}>Memory</th>
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
            <HostRow
              key={h.name}
              host={h}
              selected={i === selIdx}
              expanded={expanded.has(h.name)}
              onToggleExpand={() => toggleExpand(h.name)}
              rowPad={rowPad}
              cellPad={cellPad}
              sparkH={sparkH}
            />
          ))}
        </tbody>
      </table>
      <div className="border-t border-line-subtle px-4 py-2 text-xs text-ink-muted">
        {hosts.length} {hosts.length === 1 ? "host" : "hosts"}
        <span className="ml-3 text-ink-faint">
          <kbd className="rounded border border-line px-1">j</kbd>
          <kbd className="ml-0.5 rounded border border-line px-1">k</kbd> navigate ·
          <kbd className="ml-1 rounded border border-line px-1">↵</kbd> open ·
          <kbd className="ml-1 rounded border border-line px-1">→</kbd> expand
        </span>
      </div>
    </div>
  );
}

// --- helpers for series extraction ---

function tail(arr: (number | null)[], n: number): (number | null)[] {
  return arr.length > n ? arr.slice(arr.length - n) : arr;
}
function lastVal(arr: (number | null)[]): number | null {
  for (let i = arr.length - 1; i >= 0; i--) {
    if (arr[i] != null && !isNaN(arr[i] as number)) return arr[i] as number;
  }
  return null;
}
function prevVal(arr: (number | null)[]): number | null {
  let found = false;
  for (let i = arr.length - 1; i >= 0; i--) {
    if (arr[i] != null && !isNaN(arr[i] as number)) {
      if (found) return arr[i] as number;
      found = true;
    }
  }
  return null;
}

// --- per-host row (own metrics hook) ---

interface RowProps {
  host: Host;
  selected: boolean;
  expanded: boolean;
  onToggleExpand: () => void;
  rowPad: string;
  cellPad: string;
  sparkH: number;
}

const HostRow = memo(function HostRow({ host, selected, expanded, onToggleExpand, rowPad, cellPad, sparkH }: RowProps) {
  const navigate = useNavigate();
  const { data } = useHostMetrics(host.name, "15m");
  const cpu = data?.series.cpu_total ?? [];
  const mem = data?.series.mem_used_pct ?? [];
  const netRx = data?.series.net_rx_bps ?? [];
  const netTx = data?.series.net_tx_bps ?? [];

  const cpuTail = tail(cpu, 60);
  const memTail = tail(mem, 60);
  const cpuNow = lastVal(cpu);
  const cpuPrev = prevVal(cpu);
  const memNow = lastVal(mem);
  const memPrev = prevVal(mem);

  const open = () => navigate({ to: "/hosts/$host/metrics", params: { host: host.name } });

  return (
    <>
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
          <div className="flex items-center gap-2">
            <button
              onClick={(e) => {
                e.stopPropagation();
                onToggleExpand();
              }}
              className="flex h-5 w-5 items-center justify-center rounded text-ink-muted hover:bg-surface-overlay hover:text-ink-secondary"
              aria-label={expanded ? "Collapse" : "Expand"}
            >
              <svg
                width="12" height="12" viewBox="0 0 12 12" fill="none"
                className={`transition-transform ${expanded ? "rotate-90" : ""}`}
              >
                <path d="M4 2L8 6L4 10" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            </button>
            <Link
              to="/hosts/$host/metrics"
              params={{ host: host.name }}
              onClick={(e) => e.stopPropagation()}
              className="font-medium text-accent hover:text-accent-hover transition-colors"
            >
              {host.name}
            </Link>
          </div>
        </td>
        <td className={`${cellPad} ${rowPad}`}>
          <div className="flex items-center gap-2">
            <Sparkline data={cpuTail} width={72} height={sparkH} />
            <div className="flex flex-col leading-tight">
              <span className="tabular-nums text-ink-primary">{percent(cpuNow, 0)}</span>
              <DiffChip current={cpuNow} previous={cpuPrev} polarity="higher-is-worse" format={(d) => `${d.toFixed(1)}%`} />
            </div>
          </div>
        </td>
        <td className={`${cellPad} ${rowPad}`}>
          <div className="flex items-center gap-2">
            <Sparkline data={memTail} color="var(--status-degraded)" width={72} height={sparkH} />
            <div className="flex flex-col leading-tight">
              <span className="tabular-nums text-ink-primary">{percent(memNow, 0)}</span>
              <DiffChip current={memNow} previous={memPrev} polarity="higher-is-worse" format={(d) => `${d.toFixed(1)}%`} />
            </div>
          </div>
        </td>
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
      {expanded && (
        <tr className="border-l-2 border-l-accent/40">
          <td colSpan={7} className="bg-sunken/40 px-6 py-4">
            <HostRowExpanded host={host} cpu={cpu} mem={mem} netRx={netRx} netTx={netTx} />
          </td>
        </tr>
      )}
    </>
  );
});

// --- expanded inline panel ---

function HostRowExpanded({
  host,
  cpu,
  mem,
  netRx,
  netTx,
}: {
  host: Host;
  cpu: (number | null)[];
  mem: (number | null)[];
  netRx: (number | null)[];
  netTx: (number | null)[];
}) {
  const cpuNow = lastVal(cpu);
  const memNow = lastVal(mem);
  const rxNow = lastVal(netRx);
  const txNow = lastVal(netTx);
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
      <ExpandedMetric label="CPU" value={percent(cpuNow, 1)} data={tail(cpu, 120)} />
      <ExpandedMetric label="Memory" value={percent(memNow, 1)} data={tail(mem, 120)} color="var(--status-degraded)" />
      <ExpandedMetric
        label="Network"
        value={`${rate(rxNow)} ↓ / ${rate(txNow)} ↑`}
        data={tail(netRx, 120)}
        data2={tail(netTx, 120)}
        color="var(--status-healthy)"
        color2="var(--status-degraded)"
      />
      <div className="md:col-span-3 flex flex-wrap items-center gap-x-6 gap-y-1 border-t border-line-subtle pt-3 text-xs text-ink-muted">
        {host.role && <span>Role: <span className="text-ink-secondary">{host.role}</span></span>}
        {host.os && <span>OS: <span className="text-ink-secondary">{host.os}</span></span>}
        {host.ts_dns_name && <span className="font-mono text-ink-secondary">{host.ts_dns_name}</span>}
        {host.ts_ips?.[0] && <span className="font-mono text-ink-secondary">{host.ts_ips[0]}</span>}
        {host.uptime_seconds ? <span>Uptime: <span className="text-ink-secondary">{duration(host.uptime_seconds)}</span></span> : null}
      </div>
    </div>
  );
}

function ExpandedMetric({
  label,
  value,
  data,
  data2,
  color = "var(--accent)",
  color2,
}: {
  label: string;
  value: string;
  data: (number | null)[];
  data2?: (number | null)[];
  color?: string;
  color2?: string;
}) {
  return (
    <div>
      <div className="mb-1 flex items-center justify-between">
        <span className="metric-label">{label}</span>
        <span className="text-xs tabular-nums text-ink-secondary">{value}</span>
      </div>
      <Sparkline data={data} data2={data2} color={color} color2={color2} width={260} height={40} />
    </div>
  );
}
