import { useHostProcesses } from "@/lib/api/hooks";
import { bytes } from "@/lib/format";

export function ProcessTable({ host }: { host: string }) {
  const { data, isLoading, error } = useHostProcesses(host);

  if (isLoading) return <div className="text-gray-500">Loading processes…</div>;
  if (error) return <div className="text-red-600">Error: {error.message}</div>;
  if (!data) return null;

  return (
    <div>
      <div className="mb-3 text-sm text-gray-500 dark:text-gray-400">
        Top {data.truncated_to} by CPU on this host
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-gray-200 text-left text-xs uppercase text-gray-500 dark:border-gray-800 dark:text-gray-400">
              <th className="py-2 pr-4">PID</th>
              <th className="py-2 pr-4">Name</th>
              <th className="py-2 pr-4">Status</th>
              <th className="py-2 pr-4 text-right">CPU %</th>
              <th className="py-2 pr-4 text-right">RSS</th>
              <th className="py-2 pr-4">Command</th>
            </tr>
          </thead>
          <tbody>
            {data.processes.map((p) => (
              <tr key={p.pid} className="border-b border-gray-100 dark:border-gray-900">
                <td className="py-2 pr-4 font-mono text-xs">{p.pid}</td>
                <td className="py-2 pr-4 font-medium">{p.name}</td>
                <td className="py-2 pr-4 text-gray-500 dark:text-gray-400">{p.status}</td>
                <td className="py-2 pr-4 text-right font-mono">{p.cpu_percent.toFixed(1)}</td>
                <td className="py-2 pr-4 text-right font-mono text-xs">{bytes(p.memory_rss_bytes)}</td>
                <td className="py-2 pr-4 font-mono text-xs text-gray-500 dark:text-gray-400">{p.cmdline || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
