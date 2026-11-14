import { useState } from "react";
import { useHostContainers, useHostSwarmServices, useHostComposeProjects, useHostDockerImages } from "@/lib/api/hooks";
import { useQuery } from "@tanstack/react-query";
import { api, type ContainerStats, type Container } from "@/lib/api/client";
import { StateChip } from "@/components/StateChip";
import { useChartColors } from "@/lib/chartColors";
import { ScatterChart, Scatter, XAxis, YAxis, ZAxis, CartesianGrid, Tooltip, ResponsiveContainer } from "recharts";
import { ContainerDrawer } from "./ContainerDrawer";

/*
 * DockerTab — the host's container explorer. Redesigned per the spec:
 *   - a CPU-vs-Memory scatter summary (outlier hunting, Datadog-style) above
 *   - the container inventory table with a unified StateChip + a Health column
 *     (parsed from the human-readable status string until the backend exposes
 *     Container.Health natively — Phase B)
 * Images / Swarm / Compose sub-tabs are retained as-is.
 */

type SubTab = "containers" | "images" | "swarm" | "compose";

export function DockerTab({ host }: { host: string }) {
  const [sub, setSub] = useState<SubTab>("containers");
  const [selectedContainer, setSelectedContainer] = useState<string | null>(null);

  return (
    <div className="space-y-4">
      <div className="flex gap-1">
        {(["containers", "images", "swarm", "compose"] as const).map((s) => (
          <button
            key={s}
            onClick={() => setSub(s)}
            className={`btn capitalize ${sub === s ? "btn-primary" : "btn-ghost"}`}
          >
            {s}
          </button>
        ))}
      </div>
      {sub === "containers" && (
        <ContainersTable host={host} onSelect={setSelectedContainer} />
      )}
      {sub === "images" && <ImagesTable host={host} />}
      {sub === "swarm" && <SwarmTable host={host} />}
      {sub === "compose" && <ComposeTable host={host} />}
      {selectedContainer && (
        <ContainerDrawer
          host={host}
          containerID={selectedContainer}
          onClose={() => setSelectedContainer(null)}
        />
      )}
    </div>
  );
}

function ContainersTable({ host, onSelect }: { host: string; onSelect: (id: string) => void }) {
  const { data, isLoading, error } = useHostContainers(host);
  // Fetch container stats (live-updated via SSE container.stats events).
  const { data: statsData } = useQuery({
    queryKey: ["container-stats", host],
    queryFn: () => api.hostContainerStats(host),
  });

  if (isLoading) return <div className="space-y-1">{[0,1,2,3,4].map(i => <div key={i} className="skeleton h-12 rounded-lg" />)}</div>;
  if (error) {
    if ((error as any).type === "capability-unavailable") {
      return <div className="card text-status-degraded">Docker is not enabled on this host.</div>;
    }
    return <div className="card text-danger">Error: {error.message}</div>;
  }
  if (!data || !data.containers || data.containers.length === 0) {
    return <div className="card py-12 text-center text-zinc-500"><p className="text-sm">No containers</p></div>;
  }

  const stats = statsData?.stats ?? {};

  return (
    <div className="space-y-4">
      {/* CPU vs Memory scatter — outlier hunting at a glance */}
      <ContainerScatter containers={data.containers} stats={stats} />

      <div className="card !p-0 overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-xs uppercase tracking-wider text-zinc-500 border-b border-line-subtle">
              <th className="px-4 py-3 font-medium">State</th>
              <th className="px-4 py-3 font-medium">Name</th>
              <th className="px-4 py-3 font-medium">Image</th>
              <th className="px-4 py-3 font-medium text-right">CPU</th>
              <th className="px-4 py-3 font-medium text-right">Memory</th>
              <th className="px-4 py-3 font-medium">Status</th>
              <th className="px-4 py-3 font-medium">Health</th>
              <th className="px-4 py-3 font-medium">Ports</th>
            </tr>
          </thead>
          <tbody>
            {data.containers.map((c) => {
              const s = stats[c.id] as ContainerStats | undefined;
              const health = c.health || parseHealth(c.status);
              return (
                <tr
                  key={c.id}
                  onClick={() => onSelect(c.id)}
                  className="table-row cursor-pointer"
                >
                  <td className="px-4 py-3"><StateChip state={c.state} variant="badge" /></td>
                  <td className="px-4 py-3 font-medium text-zinc-200">{c.name}</td>
                  <td className="px-4 py-3 font-mono text-xs text-zinc-500">{c.image}</td>
                  <td className="px-4 py-3 text-right font-mono text-xs">
                    {s ? (
                      <span className={s.cpu_percent > 50 ? "text-status-degraded" : "text-zinc-300"}>
                        {s.cpu_percent.toFixed(1)}%
                      </span>
                    ) : "—"}
                  </td>
                  <td className="px-4 py-3 text-right font-mono text-xs">
                    {s ? (
                      <span className="text-zinc-300">
                        {formatBytes(s.mem_used_bytes)}
                      </span>
                    ) : "—"}
                  </td>
                  <td className="px-4 py-3 text-zinc-400">{c.status || "—"}</td>
                  <td className="px-4 py-3">
                    {health ? <StateChip state={health} variant="badge" /> : <span className="text-ink-faint">—</span>}
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-zinc-500">
                    {(c.ports ?? []).map((p, i) => (
                      <div key={i}>
                        {p.public ? `${p.ip || "0.0.0.0"}:${p.public}->` : ""}
                        {p.private}/{p.proto}
                      </div>
                    ))}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
        <div className="border-t border-line-subtle px-4 py-2 text-xs text-zinc-500">
          {data.containers.length} containers
        </div>
      </div>
    </div>
  );
}

/*
 * ContainerScatter — each running container plotted as a dot: CPU % on X,
 * memory (MiB) on Y, coloured by state. One Scatter series per state so the
 * legend doubles as a state key. Surfaces the "one container is burning CPU
 * and memory" outlier that a flat table hides.
 */
function ContainerScatter({ containers, stats }: { containers: Container[]; stats: Record<string, ContainerStats> }) {
  const colors = useChartColors();
  const stateColor: Record<string, string> = {
    running: colors.healthy,
    exited: colors.offline,
    paused: colors.degraded,
    restarting: colors.accent,
    created: colors.noagent,
    dead: colors.danger,
  };

  const points = containers
    .filter((c) => stats[c.id])
    .map((c) => {
      const s = stats[c.id];
      return { name: c.name, cpu: s.cpu_percent, mem: s.mem_used_bytes / (1024 * 1024), state: c.state };
    });

  if (points.length < 2) return null;

  const groups = new Map<string, typeof points>();
  for (const p of points) {
    const arr = groups.get(p.state) ?? [];
    arr.push(p);
    groups.set(p.state, arr);
  }

  return (
    <div className="card">
      <div className="mb-3 flex items-center justify-between">
        <h3 className="text-sm font-medium text-ink-secondary">CPU vs Memory</h3>
        <span className="text-xs text-ink-muted">each dot is a container · coloured by state</span>
      </div>
      <div style={{ height: 220 }}>
        <ResponsiveContainer width="100%" height="100%">
          <ScatterChart margin={{ top: 8, right: 16, left: 0, bottom: 8 }}>
            <CartesianGrid strokeDasharray="3 3" stroke={colors.grid} />
            <XAxis
              type="number"
              dataKey="cpu"
              name="CPU"
              domain={[0, "dataMax"]}
              stroke={colors.axis}
              tick={{ fontSize: 11, fill: colors.tick }}
              tickFormatter={(v: number) => `${v.toFixed(0)}%`}
              tickLine={false}
              axisLine={false}
            />
            <YAxis
              type="number"
              dataKey="mem"
              name="Memory"
              stroke={colors.axis}
              tick={{ fontSize: 11, fill: colors.tick }}
              tickFormatter={(v: number) => `${v.toFixed(0)}`}
              tickLine={false}
              axisLine={false}
            />
            <ZAxis range={[70, 70]} />
            <Tooltip cursor={{ strokeDasharray: "3 3" }} content={<ScatterTooltip />} />
            {[...groups.entries()].map(([state, data]) => (
              <Scatter key={state} name={state} data={data} fill={stateColor[state] ?? colors.grid} isAnimationActive={false} />
            ))}
          </ScatterChart>
        </ResponsiveContainer>
      </div>
    </div>
  );
}

interface ScatterTooltipProps {
  active?: boolean;
  payload?: { payload: { name: string; cpu: number; mem: number; state: string } }[];
}

function ScatterTooltip({ active, payload }: ScatterTooltipProps) {
  if (!active || !payload?.length) return null;
  const p = payload[0].payload;
  return (
    <div className="rounded-lg border border-line bg-surface px-3 py-2 text-xs shadow-xl">
      <div className="text-zinc-200">{p.name}</div>
      <div className="mt-1 text-zinc-400">
        CPU <span className="text-zinc-200">{p.cpu.toFixed(1)}%</span> · Mem{" "}
        <span className="text-zinc-200">{p.mem.toFixed(0)} MiB</span>
      </div>
    </div>
  );
}

// Parse Docker's health state out of the human-readable status string (e.g.
// "Up 5 minutes (healthy)") until the backend exposes Container.Health natively.
function parseHealth(status?: string): string | null {
  if (!status) return null;
  const m = status.match(/\(health:\s*([a-z]+)\)|\(([a-z]+)\)/i);
  if (!m) return null;
  const v = (m[1] ?? m[2]).toLowerCase();
  if (["healthy", "unhealthy", "starting"].includes(v)) return v;
  return null;
}

function ImagesTable({ host }: { host: string }) {
  const { data, isLoading, error } = useHostDockerImages(host);
  if (isLoading) return <div className="space-y-1">{[0,1,2,3,4].map(i => <div key={i} className="skeleton h-12 rounded-lg" />)}</div>;
  if (error) {
    if ((error as any).type === "capability-unavailable") {
      return <div className="card text-status-degraded">Docker is not enabled on this host.</div>;
    }
    return <div className="card text-danger">Error: {error.message}</div>;
  }
  const images = data?.images ?? [];
  if (images.length === 0) {
    return <div className="card py-12 text-center text-zinc-500"><p className="text-sm">No images</p></div>;
  }

  return (
    <div className="card !p-0 overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-xs uppercase tracking-wider text-zinc-500 border-b border-line-subtle">
            <th className="px-4 py-3 font-medium">Tags</th>
            <th className="px-4 py-3 font-medium">ID</th>
            <th className="px-4 py-3 font-medium">Size</th>
            <th className="px-4 py-3 font-medium">Created</th>
          </tr>
        </thead>
        <tbody>
          {images.map((img) => (
            <tr key={img.id} className="table-row">
              <td className="px-4 py-3 font-mono text-xs text-zinc-300">
                {(img.tags ?? ["<none>"]).map((t) => (
                  <div key={t}>{t}</div>
                ))}
              </td>
              <td className="px-4 py-3 font-mono text-xs text-zinc-500">
                {img.id.replace("sha256:", "").substring(0, 12)}
              </td>
              <td className="px-4 py-3 text-zinc-400">
                {img.size ? formatImageSize(img.size) : "—"}
              </td>
              <td className="px-4 py-3 text-zinc-500">
                {img.created_at ? new Date(img.created_at * 1000).toLocaleDateString() : "—"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="border-t border-line-subtle px-4 py-2 text-xs text-zinc-500">
        {images.length} images
      </div>
    </div>
  );
}

function SwarmTable({ host }: { host: string }) {
  const { data, isLoading, error } = useHostSwarmServices(host);
  if (isLoading) return <div className="space-y-1">{[0,1,2,3].map(i => <div key={i} className="skeleton h-12 rounded-lg" />)}</div>;
  if (error) {
    if ((error as any).type === "capability-unavailable" || (error as any).type === "permission-denied") {
      return <div className="card text-status-degraded">Swarm is not available: {(error as any).detail}</div>;
    }
    return <div className="card text-danger">Error: {error.message}</div>;
  }
  const services = data?.services ?? [];
  if (services.length === 0) {
    return <div className="card py-12 text-center text-zinc-500"><p className="text-sm">No swarm services</p></div>;
  }

  return (
    <div className="card !p-0 overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-xs uppercase tracking-wider text-zinc-500 border-b border-line-subtle">
            <th className="px-4 py-3 font-medium">Name</th>
            <th className="px-4 py-3 font-medium">Image</th>
            <th className="px-4 py-3 font-medium">Mode</th>
            <th className="px-4 py-3 font-medium">Replicas</th>
          </tr>
        </thead>
        <tbody>
          {services.map((s) => (
            <tr key={s.id} className="table-row">
              <td className="px-4 py-3 font-medium text-zinc-200">{s.name}</td>
              <td className="px-4 py-3 font-mono text-xs text-zinc-500">{s.image}</td>
              <td className="px-4 py-3 text-zinc-400">{s.mode || "—"}</td>
              <td className="px-4 py-3 text-zinc-400">{s.replicas ?? "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ComposeTable({ host }: { host: string }) {
  const { data, isLoading, error } = useHostComposeProjects(host);
  if (isLoading) return <div className="space-y-1">{[0,1,2].map(i => <div key={i} className="skeleton h-12 rounded-lg" />)}</div>;
  if (error) {
    if ((error as any).type === "capability-unavailable" || (error as any).type === "permission-denied") {
      return <div className="card text-status-degraded">Compose is not available: {(error as any).detail}</div>;
    }
    return <div className="card text-danger">Error: {error.message}</div>;
  }
  const projects = data?.projects ?? [];
  if (projects.length === 0) {
    return <div className="card py-12 text-center text-zinc-500"><p className="text-sm">No compose projects</p></div>;
  }

  return (
    <div className="card !p-0 overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-xs uppercase tracking-wider text-zinc-500 border-b border-line-subtle">
            <th className="px-4 py-3 font-medium">Name</th>
            <th className="px-4 py-3 font-medium">Status</th>
            <th className="px-4 py-3 font-medium">Config files</th>
          </tr>
        </thead>
        <tbody>
          {projects.map((p, i) => (
            <tr key={`${p.name}:${i}`} className="table-row">
              <td className="px-4 py-3 font-medium text-zinc-200">{p.name}</td>
              <td className="px-4 py-3 text-zinc-400">{p.status || "—"}</td>
              <td className="px-4 py-3 font-mono text-xs text-zinc-500">{p.config_files || "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function formatBytes(v: number | undefined): string {
  if (v == null || isNaN(v) || v === 0) return "—";
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

function formatImageSize(bytes: number): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let n = bytes;
  while (n >= 1000 && i < units.length - 1) {
    n /= 1000;
    i++;
  }
  if (i === 0) return `${n} ${units[i]}`;
  return `${n.toFixed(1)} ${units[i]}`;
}
