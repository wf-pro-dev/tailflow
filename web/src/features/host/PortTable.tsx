import { useHostPorts } from "@/lib/api/hooks";

export function PortTable({ host }: { host: string }) {
  const { data, isLoading, error } = useHostPorts(host);

  if (isLoading) return <div className="text-gray-500">Loading ports…</div>;
  if (error) return <div className="text-red-600">Error: {error.message}</div>;
  if (!data || data.ports.length === 0) {
    return <div className="text-gray-500">No listening ports discovered.</div>;
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-gray-200 text-left text-xs uppercase text-gray-500 dark:border-gray-800 dark:text-gray-400">
            <th className="py-2 pr-4">Port</th>
            <th className="py-2 pr-4">Proto</th>
            <th className="py-2 pr-4">Address</th>
            <th className="py-2 pr-4">PID</th>
            <th className="py-2 pr-4">Process</th>
          </tr>
        </thead>
        <tbody>
          {data.ports.map((p, i) => (
            <tr key={`${p.addr}:${p.port}:${p.proto}:${p.pid}:${i}`} className="border-b border-gray-100 dark:border-gray-900">
              <td className="py-2 pr-4 font-mono">{p.port}</td>
              <td className="py-2 pr-4 font-mono text-xs">{p.proto}</td>
              <td className="py-2 pr-4 font-mono text-xs">{p.addr}</td>
              <td className="py-2 pr-4 font-mono text-xs">{p.pid}</td>
              <td className="py-2 pr-4">{p.process || "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
