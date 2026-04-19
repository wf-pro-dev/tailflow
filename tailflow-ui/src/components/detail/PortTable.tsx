import type { ListenPort } from '../../api/types'

interface PortTableProps {
  ports: ListenPort[]
}

export function PortTable(props: PortTableProps) {
  return (
    <section className="space-y-3">
      <div>
       
        <p className="mt-1 text-sm text-zinc-600">
          Listening sockets from the latest topology snapshot.
        </p>
      </div>

      {props.ports.length === 0 ? (
        <EmptyTableState message="No listening ports were captured for this node." />
      ) : (
        <div className="overflow-hidden rounded-2xl border border-zinc-200">
          <table className="min-w-full divide-y divide-zinc-200 text-sm">
            <thead className="bg-canvas">
              <tr className="text-left text-[11px] uppercase tracking-[0.18em] text-zinc-500">
                <th className="px-4 py-3 font-medium">Port</th>
                <th className="px-4 py-3 font-medium">Proto</th>
                <th className="px-4 py-3 font-medium">Address</th>
                <th className="px-4 py-3 font-medium">Process</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-100 bg-white">
              {props.ports.map((port) => (
                <tr
                  key={`${port.addr}-${port.port}-${port.proto}-${port.pid}-${port.process}`}
                >
                  <td className="px-4 py-3 font-medium text-zinc-950">{port.port}</td>
                  <td className="px-4 py-3 text-zinc-600">{port.proto}</td>
                  <td className="px-4 py-3 text-zinc-600">{port.addr}</td>
                  <td className="px-4 py-3 text-zinc-600">
                    {port.process || (port.pid > 0 ? `pid ${port.pid}` : 'Unknown')}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

function EmptyTableState(props: { message: string }) {
  return (
    <div className="rounded-2xl border border-dashed border-zinc-200 px-4 py-5 text-sm text-zinc-500">
      {props.message}
    </div>
  )
}
