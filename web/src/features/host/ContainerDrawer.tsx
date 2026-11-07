import { useState, useEffect, useRef, useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";

interface Props {
  host: string;
  containerID: string;
  onClose: () => void;
}

type SubTab = "overview" | "stats" | "logs";

export function ContainerDrawer({ host, containerID, onClose }: Props) {
  const [sub, setSub] = useState<SubTab>("overview");

  return (
    <div className="fixed inset-y-0 right-0 w-full max-w-2xl bg-white shadow-xl dark:bg-gray-900 flex flex-col">
      <div className="flex items-center justify-between border-b border-gray-200 px-4 py-3 dark:border-gray-800">
        <div className="flex gap-1">
          {(["overview", "stats", "logs"] as const).map((s) => (
            <button
              key={s}
              onClick={() => setSub(s)}
              className={`btn capitalize ${sub === s ? "bg-blue-600 text-white dark:bg-blue-600 dark:text-white" : ""}`}
            >
              {s}
            </button>
          ))}
        </div>
        <button onClick={onClose} className="btn" aria-label="Close drawer">
          ✕
        </button>
      </div>
      <div className="flex-1 overflow-auto p-4">
        {sub === "overview" && <OverviewPanel host={host} containerID={containerID} />}
        {sub === "stats" && <StatsPanel host={host} containerID={containerID} />}
        {sub === "logs" && <LogViewer host={host} containerID={containerID} />}
      </div>
    </div>
  );
}

// --- Overview ---------------------------------------------------------------

function OverviewPanel({ host, containerID }: { host: string; containerID: string }) {
  const { data, isLoading, error } = useContainerInspect(host, containerID);
  if (isLoading) return <div className="text-gray-500">Loading…</div>;
  if (error) return <div className="text-red-600">Error: {error.message}</div>;
  if (!data) return null;

  return (
    <div className="space-y-3 text-sm">
      <Row label="ID" value={data.id} mono />
      <Row label="Name" value={data.name} />
      <Row label="Image" value={data.image} mono />
      <Row label="State" value={data.state} />
      <Row label="Status" value={data.status || "—"} />
      {data.health && <Row label="Health" value={data.health} />}
      {data.command && <Row label="Command" value={data.command} mono />}
      {data.created_at && <Row label="Created" value={new Date(data.created_at).toLocaleString()} />}
      {data.labels && Object.keys(data.labels).length > 0 && (
        <div>
          <div className="text-xs font-medium uppercase text-gray-500 dark:text-gray-400">Labels</div>
          <div className="mt-1 space-y-1">
            {Object.entries(data.labels).map(([k, v]) => (
              <div key={k} className="font-mono text-xs">
                <span className="text-gray-500 dark:text-gray-400">{k}</span>: {v}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function Row({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex gap-2">
      <div className="w-24 text-xs font-medium uppercase text-gray-500 dark:text-gray-400">{label}</div>
      <div className={`flex-1 ${mono ? "font-mono text-xs" : ""}`}>{value}</div>
    </div>
  );
}

// --- Stats ------------------------------------------------------------------

interface StatsData {
  cpu_percent?: number;
  mem_used_bytes?: number;
  mem_limit_bytes?: number;
  mem_pct?: number;
  net_rx_bytes?: number;
  net_tx_bytes?: number;
  block_read_bytes?: number;
  block_write_bytes?: number;
}

function StatsPanel({ host, containerID }: { host: string; containerID: string }) {
  const [stats, setStats] = useState<StatsData | null>(null);
  const [connected, setConnected] = useState(false);
  const [history, setHistory] = useState<StatsData[]>([]);
  const esRef = useRef<EventSource | null>(null);

  useEffect(() => {
    const url = `/api/v1/hosts/${encodeURIComponent(host)}/docker/containers/${containerID}/stats`;
    const es = new EventSource(url);
    esRef.current = es;

    es.onopen = () => setConnected(true);
    es.onerror = () => setConnected(false);

    es.addEventListener("stats.snapshot", (e) => {
      try {
        const data = JSON.parse(e.data);
        // The data is a Docker StatsResponse; we extract the key fields.
        // The exact JSON shape depends on the Docker SDK; we do a best-effort
        // extraction here and let the UI degrade gracefully if fields are
        // missing.
        const extracted = extractStats(data);
        setStats(extracted);
        setHistory((h) => {
          const next = [...h, extracted];
          if (next.length > 60) next.shift();
          return next;
        });
      } catch {
        // ignore parse errors
      }
    });

    return () => {
      es.close();
      esRef.current = null;
    };
  }, [host, containerID]);

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2 text-sm">
        <span className={`status-dot ${connected ? "status-healthy" : "status-offline"}`} />
        {connected ? "Live" : "Disconnected"}
      </div>
      {stats && (
        <div className="grid grid-cols-2 gap-4">
          <StatCard label="CPU" value={`${(stats.cpu_percent ?? 0).toFixed(1)}%`} />
          <StatCard label="Memory" value={formatBytes(stats.mem_used_bytes)} sub={stats.mem_limit_bytes ? `/ ${formatBytes(stats.mem_limit_bytes)}` : ""} />
          <StatCard label="Net RX" value={formatBytes(stats.net_rx_bytes)} />
          <StatCard label="Net TX" value={formatBytes(stats.net_tx_bytes)} />
          <StatCard label="Block Read" value={formatBytes(stats.block_read_bytes)} />
          <StatCard label="Block Write" value={formatBytes(stats.block_write_bytes)} />
        </div>
      )}
      {history.length > 1 && (
        <div className="card">
          <h3 className="mb-2 text-sm font-medium">CPU (last {history.length} samples)</h3>
          <MiniChart data={history.map((h) => h.cpu_percent ?? 0)} />
        </div>
      )}
    </div>
  );
}

function StatCard({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div className="card">
      <div className="text-xs font-medium uppercase text-gray-500 dark:text-gray-400">{label}</div>
      <div className="mt-1 text-lg font-bold">{value}</div>
      {sub && <div className="text-xs text-gray-500 dark:text-gray-400">{sub}</div>}
    </div>
  );
}

function MiniChart({ data }: { data: number[] }) {
  const max = Math.max(...data, 1);
  return (
    <div className="flex h-16 items-end gap-px">
      {data.map((v, i) => (
        <div
          key={i}
          className="flex-1 bg-blue-500"
          style={{ height: `${(v / max) * 100}%`, minHeight: "1px" }}
        />
      ))}
    </div>
  );
}

// extractStats pulls the key fields from a Docker StatsResponse. The shape
// varies by Docker version; we do a defensive best-effort extraction.
function extractStats(data: any): StatsData {
  const cpu = data?.cpu_stats ?? {};
  const precpu = data?.precpu_stats ?? {};
  const mem = data?.memory_stats ?? {};
  const net = data?.networks ?? {};
  const blkio = data?.blkio_stats ?? {};

  // CPU %: (Δcpu_total / Δsystem_cpu) * online_cpus * 100
  let cpuPercent = 0;
  const cpuTotal = cpu?.cpu_usage?.total_usage ?? 0;
  const precpuTotal = precpu?.cpu_usage?.total_usage ?? 0;
  const sysTotal = cpu?.system_cpu_usage ?? 0;
  const preSysTotal = precpu?.system_cpu_usage ?? 0;
  const onlineCpus = cpu?.online_cpus ?? 1;
  if (sysTotal > preSysTotal && cpuTotal > precpuTotal) {
    cpuPercent = ((cpuTotal - precpuTotal) / (sysTotal - preSysTotal)) * onlineCpus * 100;
  }

  // Memory: usage - inactive_file (cache-adjusted)
  const memUsed = mem?.usage ?? 0;
  const memLimit = mem?.limit ?? 0;
  const memPct = memLimit > 0 ? (memUsed / memLimit) * 100 : 0;

  // Network: sum across interfaces
  let netRx = 0, netTx = 0;
  if (Array.isArray(net)) {
    for (const n of net) {
      netRx += n?.rx_bytes ?? 0;
      netTx += n?.tx_bytes ?? 0;
    }
  } else if (net && typeof net === "object") {
    for (const k of Object.keys(net)) {
      netRx += net[k]?.rx_bytes ?? 0;
      netTx += net[k]?.tx_bytes ?? 0;
    }
  }

  // Block I/O: sum io_service_bytes_recursive
  let blockRead = 0, blockWrite = 0;
  if (Array.isArray(blkio?.io_service_bytes_recursive)) {
    for (const b of blkio.io_service_bytes_recursive) {
      if (b?.op === "Read") blockRead += b?.value ?? 0;
      if (b?.op === "Write") blockWrite += b?.value ?? 0;
    }
  }

  return {
    cpu_percent: cpuPercent,
    mem_used_bytes: memUsed,
    mem_limit_bytes: memLimit,
    mem_pct: memPct,
    net_rx_bytes: netRx,
    net_tx_bytes: netTx,
    block_read_bytes: blockRead,
    block_write_bytes: blockWrite,
  };
}

// --- Logs -------------------------------------------------------------------

function LogViewer({ host, containerID }: { host: string; containerID: string }) {
  const [lines, setLines] = useState<string[]>([]);
  const [follow, setFollow] = useState(true);
  const [filter, setFilter] = useState("");
  const [connected, setConnected] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const esRef = useRef<EventSource | null>(null);

  useEffect(() => {
    if (!follow) {
      esRef.current?.close();
      esRef.current = null;
      setConnected(false);
      return;
    }

    const url = `/api/v1/hosts/${encodeURIComponent(host)}/docker/containers/${containerID}/logs`;
    const es = new EventSource(url);
    esRef.current = es;

    es.onopen = () => setConnected(true);
    es.onerror = () => setConnected(false);

    es.addEventListener("log.line", (e) => {
      try {
        const data = JSON.parse(e.data);
        const line = typeof data === "string" ? data : data?.line ?? JSON.stringify(data);
        setLines((prev) => {
          const next = [...prev, line];
          if (next.length > 5000) next.shift();
          return next;
        });
      } catch {
        // ignore
      }
    });

    return () => {
      es.close();
      esRef.current = null;
    };
  }, [host, containerID, follow]);

  // Auto-scroll to bottom when new lines arrive (if following).
  useEffect(() => {
    if (follow && containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [lines, follow]);

  const filtered = filter ? lines.filter((l) => l.toLowerCase().includes(filter.toLowerCase())) : lines;

  return (
    <div className="flex h-full flex-col">
      <div className="mb-2 flex items-center gap-2">
        <button
          onClick={() => setFollow(!follow)}
          className={`btn ${follow ? "bg-blue-600 text-white dark:bg-blue-600 dark:text-white" : ""}`}
        >
          {follow ? "Following" : "Paused"}
        </button>
        <input
          type="text"
          placeholder="Filter…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="input flex-1"
        />
        <span className={`status-dot ${connected ? "status-healthy" : "status-offline"}`} />
      </div>
      <div ref={containerRef} className="flex-1 overflow-auto rounded bg-gray-950 p-2 font-mono text-xs text-gray-100">
        {filtered.length === 0 ? (
          <div className="text-gray-500">{connected ? "Waiting for logs…" : "Disconnected"}</div>
        ) : (
          filtered.map((line, i) => (
            <div key={i} className="whitespace-pre-wrap break-all">
              {line}
            </div>
          ))
        )}
      </div>
      <div className="mt-1 text-xs text-gray-500 dark:text-gray-400">
        {filtered.length} {filtered.length === 1 ? "line" : "lines"}
        {filter && ` (filtered from ${lines.length})`}
      </div>
    </div>
  );
}

// --- Hooks ------------------------------------------------------------------

import { useQuery } from "@tanstack/react-query";
import { api, type Container } from "@/lib/api/client";

function useContainerInspect(host: string, containerID: string) {
  return useQuery({
    queryKey: ["container", host, containerID],
    queryFn: () => api.hostContainers(host).then((list) => {
      const c = list.containers.find((c: Container) => c.id === containerID || c.id.startsWith(containerID));
      if (!c) throw new Error("Container not found");
      return c;
    }),
  });
}

// --- Helpers ----------------------------------------------------------------

function formatBytes(v: number | undefined): string {
  if (v == null || isNaN(v)) return "—";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let i = 0;
  let n = v;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  if (i === 0) return `${n} ${units[i]}`;
  return `${n.toFixed(1)} ${units[i]}`;
}
