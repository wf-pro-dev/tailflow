import type { ContainerPublishedPort, ContainerSummary } from '../../api/types'

interface ContainerTableProps {
  containers: ContainerSummary[]
}

export function ContainerTable(props: ContainerTableProps) {
  return (
    <section className="space-y-3">
      <div>
        
        <p className="mt-1 text-sm text-zinc-600">
          Docker containers on this node.
        </p>
      </div>

      {props.containers.length === 0 ? (
        <EmptyTableState message="No containers were captured for this node." />
      ) : (
        <div className="space-y-3">
          {props.containers.map((container) => (
            <div
              key={container.container_id}
              className="rounded-2xl border border-zinc-200 bg-white p-4"
            >
              <div className="flex items-start justify-between gap-4">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-zinc-950">
                    {container.container_name}
                  </p>
                  <p className="mt-1 truncate text-xs text-zinc-500">
                    {container.image}
                  </p>
                </div>
                <div className="text-right text-xs text-zinc-500">
                  <p className="font-medium text-zinc-700">{container.state}</p>
                  <p className="mt-1">{container.status}</p>
                </div>
              </div>

              <div className="mt-3 grid grid-cols-2 gap-3 text-xs text-zinc-600">
                <MetaRow label="Service" value={container.service_name || 'Standalone'} />
                <MetaRow
                  label="Publishes"
                  value={String(container.published_ports.length)}
                />
              </div>

              {container.published_ports.length > 0 ? (
                <div className="mt-4 overflow-hidden rounded-xl border border-zinc-100">
                  <table className="min-w-full divide-y divide-zinc-100 text-xs">
                    <thead className="bg-canvas">
                      <tr className="text-left uppercase tracking-[0.16em] text-zinc-500">
                        <th className="px-3 py-2 font-medium">Source</th>
                        <th className="px-3 py-2 font-medium">Host</th>
                        <th className="px-3 py-2 font-medium">Target</th>
                        <th className="px-3 py-2 font-medium">Proto</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-zinc-100 bg-white">
                      {container.published_ports.map((port) => (
                        <tr key={publishedPortKey(container.container_id, port)}>
                          <td className="px-3 py-2 text-zinc-700">
                            {port.source === 'service'
                              ? `service${port.mode ? ` (${port.mode})` : ''}`
                              : 'container'}
                          </td>
                          <td className="px-3 py-2 text-zinc-600">{port.host_port}</td>
                          <td className="px-3 py-2 text-zinc-600">{port.target_port}</td>
                          <td className="px-3 py-2 text-zinc-600">{port.proto}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              ) : (
                <p className="mt-4 rounded-xl border border-dashed border-zinc-200 px-3 py-3 text-xs text-zinc-500">
                  No published ports were associated with this container.
                </p>
              )}
            </div>
          ))}
        </div>
      )}
    </section>
  )
}

function MetaRow(props: { label: string; value: string }) {
  return (
    <div className="rounded-xl border border-zinc-100 bg-canvas px-3 py-2">
      <p className="uppercase tracking-[0.16em] text-zinc-400">{props.label}</p>
      <p className="mt-1 truncate text-sm font-medium text-zinc-900">{props.value}</p>
    </div>
  )
}

function publishedPortKey(containerID: string, port: ContainerPublishedPort) {
  return [
    containerID,
    String(port.host_port),
    String(port.target_port),
    port.proto,
    port.source,
    port.mode ?? '',
  ].join('|')
}

function EmptyTableState(props: { message: string }) {
  return (
    <div className="rounded-2xl border border-dashed border-zinc-200 px-4 py-5 text-sm text-zinc-500">
      {props.message}
    </div>
  )
}
