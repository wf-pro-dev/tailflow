import { useHostPorts } from "@/lib/api/hooks";

export function PortTable({ host }: { host: string }) {
  const { data, isLoading, error } = useHostPorts(host);

  if (isLoading) return <div className="space-y-1">{[0,1,2,3].map(i => <div key={i} className="skeleton h-12 rounded-lg" />)}</div>;
  if (error) return <div className="card text-red-400">Error: {error.message}</div>;
  if (!data || data.ports.length === 0) {
    return (
      <div className="card py-12 text-center text-zinc-500">
        <p className="text-sm">No listening ports discovered</p>
      </div>
    );
  }

  return (
    <div className="card !p-0 overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-xs uppercase tracking-wider text-zinc-500 border-b border-white/[0.04]">
            <th className="px-4 py-3 font-medium">Port</th>
            <th className="px-4 py-3 font-medium">Proto</th>
            <th className="px-4 py-3 font-medium">Address</th>
            <th className="px-4 py-3 font-medium">PID</th>
            <th className="px-4 py-3 font-medium">Process</th>
          </tr>
        </thead>
        <tbody>
          {data.ports.map((p, i) => (
            <tr key={`${p.addr}:${p.port}:${p.proto}:${p.pid}:${i}`} className="table-row">
              <td className="px-4 py-3 font-mono text-zinc-200">{p.port}</td>
              <td className="px-4 py-3 font-mono text-xs text-zinc-500">{p.proto}</td>
              <td className="px-4 py-3 font-mono text-xs text-zinc-400">{p.addr}</td>
              <td className="px-4 py-3 font-mono text-xs text-zinc-500">{p.pid}</td>
              <td className="px-4 py-3 text-zinc-300">{p.process || "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="border-t border-white/[0.04] px-4 py-2 text-xs text-zinc-500">
        {data.ports.length} listening {data.ports.length === 1 ? "socket" : "sockets"}
      </div>
    </div>
  );
}
