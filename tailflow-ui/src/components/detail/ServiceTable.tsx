import type { SwarmServicePort } from '../../api/types'

interface ServiceTableProps {
  services: SwarmServicePort[]
}

export function ServiceTable(props: ServiceTableProps) {
  return (
    <section className="space-y-3">
      <div>
        <p className="text-[12px] font-medium uppercase tracking-[0.18em] text-zinc-500">
          Services
        </p>
        <p className="mt-1 text-sm text-zinc-600">
          Published Docker Swarm service ports mapped on this node.
        </p>
      </div>

      {props.services.length === 0 ? (
        <EmptyTableState message="No published Swarm services were captured for this node." />
      ) : (
        <div className="overflow-hidden rounded-2xl border border-zinc-200">
          <table className="min-w-full divide-y divide-zinc-200 text-sm">
            <thead className="bg-canvas">
              <tr className="text-left text-[11px] uppercase tracking-[0.18em] text-zinc-500">
                <th className="px-4 py-3 font-medium">Service</th>
                <th className="px-4 py-3 font-medium">Host</th>
                <th className="px-4 py-3 font-medium">Target</th>
                <th className="px-4 py-3 font-medium">Proto</th>
                <th className="px-4 py-3 font-medium">Mode</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-100 bg-white">
              {props.services.map((service) => (
                <tr
                  key={`${service.service_id}-${service.host_port}-${service.target_port}-${service.proto}`}
                >
                  <td className="px-4 py-3 font-medium text-zinc-950">
                    {service.service_name}
                  </td>
                  <td className="px-4 py-3 text-zinc-600">{service.host_port}</td>
                  <td className="px-4 py-3 text-zinc-600">{service.target_port}</td>
                  <td className="px-4 py-3 text-zinc-600">{service.proto}</td>
                  <td className="px-4 py-3 text-zinc-600">{service.mode || 'n/a'}</td>
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
