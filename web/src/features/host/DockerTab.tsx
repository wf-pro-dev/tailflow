import { useState } from "react";
import { useHostContainers, useHostSwarmServices, useHostComposeProjects } from "@/lib/api/hooks";

type SubTab = "containers" | "swarm" | "compose";

export function DockerTab({ host }: { host: string }) {
  const [sub, setSub] = useState<SubTab>("containers");

  return (
    <div className="space-y-4">
      <div className="flex gap-1">
        {(["containers", "swarm", "compose"] as const).map((s) => (
          <button
            key={s}
            onClick={() => setSub(s)}
            className={`btn capitalize ${sub === s ? "bg-blue-600 text-white dark:bg-blue-600 dark:text-white" : ""}`}
          >
            {s}
          </button>
        ))}
      </div>
      {sub === "containers" && <ContainersTable host={host} />}
      {sub === "swarm" && <SwarmTable host={host} />}
      {sub === "compose" && <ComposeTable host={host} />}
    </div>
  );
}

function ContainersTable({ host }: { host: string }) {
  const { data, isLoading, error } = useHostContainers(host);
  if (isLoading) return <div className="text-gray-500">Loading containers…</div>;
  if (error) {
    if ((error as any).type === "capability-unavailable") {
      return <div className="text-amber-600">Docker is not enabled on this host.</div>;
    }
    return <div className="text-red-600">Error: {error.message}</div>;
  }
  if (!data || data.containers.length === 0) {
    return <div className="text-gray-500">No containers.</div>;
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-gray-200 text-left text-xs uppercase text-gray-500 dark:border-gray-800 dark:text-gray-400">
            <th className="py-2 pr-4">State</th>
            <th className="py-2 pr-4">Name</th>
            <th className="py-2 pr-4">Image</th>
            <th className="py-2 pr-4">Status</th>
            <th className="py-2 pr-4">Ports</th>
          </tr>
        </thead>
        <tbody>
          {data.containers.map((c) => (
            <tr key={c.id} className="border-b border-gray-100 dark:border-gray-900">
              <td className="py-2 pr-4">
                <StateChip state={c.state} />
              </td>
              <td className="py-2 pr-4 font-medium">{c.name}</td>
              <td className="py-2 pr-4 font-mono text-xs text-gray-500 dark:text-gray-400">{c.image}</td>
              <td className="py-2 pr-4 text-gray-500 dark:text-gray-400">{c.status || "—"}</td>
              <td className="py-2 pr-4 font-mono text-xs">
                {(c.ports ?? []).map((p, i) => (
                  <div key={i}>
                    {p.public ? `${p.ip || "0.0.0.0"}:${p.public}->` : ""}
                    {p.private}/{p.proto}
                  </div>
                ))}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function SwarmTable({ host }: { host: string }) {
  const { data, isLoading, error } = useHostSwarmServices(host);
  if (isLoading) return <div className="text-gray-500">Loading swarm services…</div>;
  if (error) {
    if ((error as any).type === "capability-unavailable" || (error as any).type === "permission-denied") {
      return <div className="text-amber-600">Swarm is not available on this host: {(error as any).detail}</div>;
    }
    return <div className="text-red-600">Error: {error.message}</div>;
  }
  if (!data || data.services.length === 0) {
    return <div className="text-gray-500">No swarm services.</div>;
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-gray-200 text-left text-xs uppercase text-gray-500 dark:border-gray-800 dark:text-gray-400">
            <th className="py-2 pr-4">Name</th>
            <th className="py-2 pr-4">Image</th>
            <th className="py-2 pr-4">Mode</th>
            <th className="py-2 pr-4">Replicas</th>
          </tr>
        </thead>
        <tbody>
          {data.services.map((s) => (
            <tr key={s.id} className="border-b border-gray-100 dark:border-gray-900">
              <td className="py-2 pr-4 font-medium">{s.name}</td>
              <td className="py-2 pr-4 font-mono text-xs text-gray-500 dark:text-gray-400">{s.image}</td>
              <td className="py-2 pr-4">{s.mode || "—"}</td>
              <td className="py-2 pr-4">{s.replicas ?? "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ComposeTable({ host }: { host: string }) {
  const { data, isLoading, error } = useHostComposeProjects(host);
  if (isLoading) return <div className="text-gray-500">Loading compose projects…</div>;
  if (error) {
    if ((error as any).type === "capability-unavailable" || (error as any).type === "permission-denied") {
      return <div className="text-amber-600">Compose is not available on this host: {(error as any).detail}</div>;
    }
    return <div className="text-red-600">Error: {error.message}</div>;
  }
  if (!data || data.projects.length === 0) {
    return <div className="text-gray-500">No compose projects.</div>;
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-gray-200 text-left text-xs uppercase text-gray-500 dark:border-gray-800 dark:text-gray-400">
            <th className="py-2 pr-4">Name</th>
            <th className="py-2 pr-4">Status</th>
            <th className="py-2 pr-4">Config files</th>
          </tr>
        </thead>
        <tbody>
          {data.projects.map((p, i) => (
            <tr key={`${p.name}:${i}`} className="border-b border-gray-100 dark:border-gray-900">
              <td className="py-2 pr-4 font-medium">{p.name}</td>
              <td className="py-2 pr-4 text-gray-500 dark:text-gray-400">{p.status || "—"}</td>
              <td className="py-2 pr-4 font-mono text-xs text-gray-500 dark:text-gray-400">{p.config_files || "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function StateChip({ state }: { state: string }) {
  const colors: Record<string, string> = {
    running: "bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400",
    exited: "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400",
    paused: "bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-400",
    restarting: "bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-400",
    created: "bg-purple-100 text-purple-800 dark:bg-purple-900/30 dark:text-purple-400",
    dead: "bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-400",
  };
  return (
    <span className={`inline-block rounded px-1.5 py-0.5 text-xs font-medium ${colors[state] || colors.exited}`}>
      {state}
    </span>
  );
}
