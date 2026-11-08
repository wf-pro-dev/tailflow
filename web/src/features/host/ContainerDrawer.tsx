import { useState, useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { MetricChart } from "@/components/MetricChart";

interface Props {
  host: string;
  containerID: string;
  onClose: () => void;
}

type SubTab = "overview" | "stats" | "logs";

export function ContainerDrawer({ host, containerID, onClose }: Props) {
  const [sub, setSub] = useState<SubTab>("overview");

  return (
    <div className="fixed inset-y-0 right-0 z-50 flex w-full max-w-2xl flex-col bg-[#09090B] shadow-2xl"
      style={{ animation: "slide-in-right 0.3s ease-out" }}
    >
      {/* Header */}
      <div className="flex items-center justify-between border-b border-white/[0.04] px-5 py-4">
        <div className="flex gap-1">
          {(["overview", "stats", "logs"] as const).map((s) => (
            <button
              key={s}
              onClick={() => setSub(s)}
              className={sub === s ? "tab-item-active" : "tab-item"}
            >
              {s}
            </button>
          ))}
        </div>
        <button
          onClick={onClose}
          className="flex h-8 w-8 items-center justify-center rounded-lg text-zinc-500 hover:bg-white/[0.05] hover:text-zinc-300 transition-colors"
          aria-label="Close"
        >
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
            <path d="M4 4L12 12M12 4L4 12" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
        </button>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-auto p-5">
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
  if (isLoading) return <div className="text-zinc-500">Loading…</div>;
  if (error) return <div className="text-red-400">Error: {error.message}</div>;
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
        <div className="pt-3 border-t border-white/[0.04]">
          <div className="metric-label mb-2">Labels</div>
          <div className="space-y-1">
            {Object.entries(data.labels).map(([k, v]) => (
              <div key={k} className="font-mono text-xs">
                <span className="text-zinc-500">{k}</span>: <span className="text-zinc-300">{v}</span>
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
    <div className="flex gap-3">
      <div className="w-24 shrink-0 text-xs font-medium uppercase tracking-wider text-zinc-500">{label}</div>
      <div className={`flex-1 ${mono ? "font-mono text-xs" : ""} text-zinc-300`}>{value}</div>
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

interface StatsHistory {
  t: number[];
  cpu: number[];
  mem: number[];
  netRx: number[];
  netTx: number[];
}

function StatsPanel({ host, containerID }: { host: string; containerID: string }) {
  const [stats, setStats] = useState<StatsData | null>(null);
  const [connected, setConnected] = useState(false);
  const [history, setHistory] = useState<StatsHistory>({ t: [], cpu: [], mem: [], netRx: [], netTx: [] });
  const sampleCount = useRef(0);

  useEffect(() => {
    const url = `/api/v1/hosts/${encodeURIComponent(host)}/docker/containers/${containerID}/stats`;
    const es = new EventSource(url);

    es.onopen = () => setConnected(true);
    es.onerror = () => setConnected(false);

    es.addEventListener("stats.snapshot", (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data);
        const extracted = extractStats(data);
        setStats(extracted);
        sampleCount.current++;
        const t = sampleCount.current;
        setHistory((h) => {
          const MAX = 120;
          const next = {
            t: [...h.t, t].slice(-MAX),
            cpu: [...h.cpu, extracted.cpu_percent ?? 0].slice(-MAX),
            mem: [...h.mem, extracted.mem_pct ?? 0].slice(-MAX),
            netRx: [...h.netRx, extracted.net_rx_bytes ?? 0].slice(-MAX),
            netTx: [...h.netTx, extracted.net_tx_bytes ?? 0].slice(-MAX),
          };
          return next;
        });
      } catch {
        // ignore parse errors
      }
    });

    return () => es.close();
  }, [host, containerID]);

  return (
    <div className="space-y-4">
      {/* Live indicator */}
      <div className="flex items-center gap-2 text-sm">
        <span className={`status-dot ${connected ? "status-dot-pulse status-healthy" : "status-offline"}`} />
        <span className={connected ? "text-green-400" : "text-zinc-500"}>
          {connected ? "Live" : "Disconnected"}
        </span>
      </div>

      {/* Current value stat cards */}
      {stats && (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
          <StatCard label="CPU" value={`${(stats.cpu_percent ?? 0).toFixed(1)}%`} color="text-blue-400" />
          <StatCard
            label="Memory"
            value={formatBytes(stats.mem_used_bytes)}
            sub={stats.mem_limit_bytes ? `/ ${formatBytes(stats.mem_limit_bytes)}` : ""}
            color="text-purple-400"
          />
          <StatCard label="Net RX" value={formatBytes(stats.net_rx_bytes)} color="text-green-400" />
          <StatCard label="Net TX" value={formatBytes(stats.net_tx_bytes)} color="text-amber-400" />
          <StatCard label="Block Read" value={formatBytes(stats.block_read_bytes)} color="text-zinc-300" />
          <StatCard label="Block Write" value={formatBytes(stats.block_write_bytes)} color="text-zinc-300" />
        </div>
      )}

      {/* Live charts */}
      {history.t.length > 1 && (
        <>
          {/* CPU chart */}
          <div className="card">
            <div className="mb-3 flex items-center justify-between">
              <h3 className="text-sm font-medium text-zinc-300">CPU</h3>
              <span className="text-xs text-zinc-500">
                {(history.cpu[history.cpu.length - 1] ?? 0).toFixed(1)}%
              </span>
            </div>
            <MetricChart
              t={history.t}
              series={[{ label: "CPU %", data: history.cpu, color: "#3DA5E0" }]}
              format={(v) => `${v.toFixed(1)}%`}
              yMax={100}
              type="area"
              height={160}
            />
          </div>

          {/* Memory chart */}
          <div className="card">
            <div className="mb-3 flex items-center justify-between">
              <h3 className="text-sm font-medium text-zinc-300">Memory</h3>
              <span className="text-xs text-zinc-500">
                {(history.mem[history.mem.length - 1] ?? 0).toFixed(1)}%
              </span>
            </div>
            <MetricChart
              t={history.t}
              series={[{ label: "Mem %", data: history.mem, color: "#6F3FE0" }]}
              format={(v) => `${v.toFixed(1)}%`}
              yMax={100}
              type="area"
              height={160}
            />
          </div>

          {/* Network chart */}
          <div className="card">
            <div className="mb-3 flex items-center justify-between">
              <h3 className="text-sm font-medium text-zinc-300">Network I/O</h3>
              <span className="text-xs text-zinc-500">
                ↓ {formatBytes(history.netRx[history.netRx.length - 1])} / {" "}
                ↑ {formatBytes(history.netTx[history.netTx.length - 1])}
              </span>
            </div>
            <MetricChart
              t={history.t}
              series={[
                { label: "RX", data: history.netRx, color: "#22C55E" },
                { label: "TX", data: history.netTx, color: "#F59E0B" },
              ]}
              format={(v) => formatBytes(v)}
              height={160}
            />
          </div>
        </>
      )}
    </div>
  );
}

function StatCard({ label, value, sub, color }: { label: string; value: string; sub?: string; color: string }) {
  return (
    <div className="card">
      <div className="metric-label">{label}</div>
      <div className={`mt-2 text-xl font-bold tracking-tight ${color}`}>{value}</div>
      {sub && <div className="mt-1 text-xs text-zinc-500">{sub}</div>}
    </div>
  );
}

// --- Logs -------------------------------------------------------------------

function LogViewer({ host, containerID }: { host: string; containerID: string }) {
  const [lines, setLines] = useState<string[]>([]);
  const [follow, setFollow] = useState(true);
  const [filter, setFilter] = useState("");
  const [connected, setConnected] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!follow) {
      return;
    }
    const url = `/api/v1/hosts/${encodeURIComponent(host)}/docker/containers/${containerID}/logs`;
    const es = new EventSource(url);

    es.onopen = () => setConnected(true);
    es.onerror = () => setConnected(false);

    es.addEventListener("log.line", (e: MessageEvent) => {
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

    return () => es.close();
  }, [host, containerID, follow]);

  // Auto-scroll when following
  useEffect(() => {
    if (follow && containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [lines, follow]);

  const filtered = filter
    ? lines.filter((l) => l.toLowerCase().includes(filter.toLowerCase()))
    : lines;

  return (
    <div className="flex h-full flex-col gap-3">
      <div className="flex items-center gap-2">
        <button
          onClick={() => setFollow(!follow)}
          className={follow ? "btn-primary" : "btn-ghost"}
        >
          {follow ? "Following" : "Paused"}
        </button>
        <input
          type="text"
          placeholder="Filter logs..."
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="input flex-1"
        />
        <span className={`status-dot ${connected ? "status-dot-pulse status-healthy" : "status-offline"}`} />
      </div>

      <div
        ref={containerRef}
        className="flex-1 overflow-auto rounded-lg border border-white/[0.04] bg-black/40 p-3 font-mono text-xs leading-relaxed text-zinc-300"
      >
        {filtered.length === 0 ? (
          <div className="text-zinc-600">
            {connected ? "Waiting for logs..." : "Disconnected"}
          </div>
        ) : (
          filtered.map((line, i) => (
            <div key={i} className="whitespace-pre-wrap break-all">
              {line}
            </div>
          ))
        )}
      </div>

      <div className="text-xs text-zinc-500">
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
    queryFn: () =>
      api.hostContainers(host).then((list) => {
        const c = list.containers.find(
          (c: Container) => c.id === containerID || c.id.startsWith(containerID),
        );
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

// extractStats pulls the key fields from a Docker StatsResponse.
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

  const memUsed = mem?.usage ?? 0;
  const memLimit = mem?.limit ?? 0;

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

  // Block I/O
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
    mem_pct: memLimit > 0 ? (memUsed / memLimit) * 100 : 0,
    net_rx_bytes: netRx,
    net_tx_bytes: netTx,
    block_read_bytes: blockRead,
    block_write_bytes: blockWrite,
  };
}
