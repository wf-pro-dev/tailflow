import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api/client";

interface SystemdUnit {
  name: string;
  load_state: string;
  active_state: string;
  sub_state: string;
  description?: string;
}

export function SystemdTab({ host }: { host: string }) {
  const { data, isLoading, error } = useQuery({
    queryKey: ["systemd", host],
    queryFn: () => api.systemdUnits(host),
    staleTime: 30_000, // systemd doesn't change often; poll every 30s
    refetchInterval: 30_000,
  });

  if (isLoading) {
    return (
      <div className="space-y-1">
        {[0, 1, 2, 3, 4].map((i) => (
          <div key={i} className="skeleton h-12 rounded-lg" />
        ))}
      </div>
    );
  }

  if (error) {
    return <div className="card text-red-400">Error: {error.message}</div>;
  }

  const units = (data as any)?.units as SystemdUnit[] | undefined;
  if (!units || units.length === 0) {
    return (
      <div className="card py-12 text-center text-zinc-500">
        <p className="text-sm">No systemd units</p>
        <p className="mt-1 text-xs text-zinc-600">
          Systemd may not be configured on this host.
        </p>
      </div>
    );
  }

  const failed = units.filter((u) => u.active_state === "failed").length;

  return (
    <div>
      <div className="mb-3 flex items-center justify-between">
        <span className="text-sm text-zinc-400">
          {units.length} units
        </span>
        {failed > 0 && (
          <span className="text-sm text-red-400">
            ⚠ {failed} failed
          </span>
        )}
      </div>
      <div className="card !p-0 overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-xs uppercase tracking-wider text-zinc-500 border-b border-white/[0.04]">
              <th className="px-4 py-3 font-medium">Unit</th>
              <th className="px-4 py-3 font-medium">Load</th>
              <th className="px-4 py-3 font-medium">Active</th>
              <th className="px-4 py-3 font-medium">Sub</th>
              <th className="px-4 py-3 font-medium">Description</th>
            </tr>
          </thead>
          <tbody>
            {units.map((u) => (
              <tr
                key={u.name}
                className={`table-row ${u.active_state === "failed" ? "bg-red-500/5" : ""}`}
              >
                <td className="px-4 py-3 font-mono text-xs text-zinc-300">{u.name}</td>
                <td className="px-4 py-3">
                  <StateChip state={u.load_state} />
                </td>
                <td className="px-4 py-3">
                  <StateChip state={u.active_state} />
                </td>
                <td className="px-4 py-3 text-zinc-400 text-xs">{u.sub_state}</td>
                <td className="px-4 py-3 text-zinc-500 text-xs">{u.description || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function StateChip({ state }: { state: string }) {
  const colors: Record<string, string> = {
    active: "text-green-400",
    running: "text-green-400",
    loaded: "text-zinc-400",
    inactive: "text-zinc-500",
    failed: "text-red-400",
    activating: "text-blue-400",
    deactivating: "text-amber-400",
    reloading: "text-blue-400",
    maintenance: "text-amber-400",
  };
  return (
    <span className={`text-xs font-medium ${colors[state] || "text-zinc-400"}`}>
      {state}
    </span>
  );
}
