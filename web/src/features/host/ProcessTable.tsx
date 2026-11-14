import { useHostProcesses } from "@/lib/api/hooks";
import { bytes } from "@/lib/format";

export function ProcessTable({ host }: { host: string }) {
  const { data, isLoading, error } = useHostProcesses(host);

  // SSE subscription for metrics:{host} is handled by the host layout.

  if (isLoading) return <div className="space-y-1">{[0,1,2,3,4].map(i => <div key={i} className="skeleton h-12 rounded-lg" />)}</div>;
  if (error) return <div className="card text-danger">Error: {error.message}</div>;
  if (!data) return null;

  return (
    <div>
      <div className="mb-3 text-sm text-zinc-500">
        Top {data.truncated_to} by CPU on this host
      </div>
      <div className="card !p-0 overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-xs uppercase tracking-wider text-zinc-500 border-b border-line-subtle">
              <th className="px-4 py-3 font-medium">PID</th>
              <th className="px-4 py-3 font-medium">Name</th>
              <th className="px-4 py-3 font-medium">Status</th>
              <th className="px-4 py-3 font-medium text-right">CPU %</th>
              <th className="px-4 py-3 font-medium text-right">RSS</th>
              <th className="px-4 py-3 font-medium">Command</th>
            </tr>
          </thead>
          <tbody>
            {data.processes.map((p) => (
              <tr key={p.pid} className="table-row">
                <td className="px-4 py-3 font-mono text-xs text-zinc-500">{p.pid}</td>
                <td className="px-4 py-3 font-medium text-zinc-200">{p.name}</td>
                <td className="px-4 py-3 text-zinc-500">{p.status}</td>
                <td className="px-4 py-3 text-right font-mono text-xs">
                  <span className={p.cpu_percent > 50 ? "text-status-degraded" : "text-zinc-300"}>
                    {p.cpu_percent.toFixed(1)}
                  </span>
                </td>
                <td className="px-4 py-3 text-right font-mono text-xs text-zinc-400">{bytes(p.memory_rss_bytes)}</td>
                <td className="px-4 py-3 font-mono text-xs text-zinc-500 max-w-xs truncate">{p.cmdline || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
