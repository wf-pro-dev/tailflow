import type { NodeResponse } from '../../api/types'
import { getNodeStatusView } from '../../lib/node-status'
import { formatRelativeTime } from '../../lib/time'
import { cn } from '../../lib/utils'
import { Spinner } from '../shared/Spinner'
import { StatusDot } from '../shared/StatusDot'

interface NodeListProps {
  nodes: NodeResponse[]
  isLoading: boolean
  selectedNodeName: string | null
  onSelectNode: (nodeName: string) => void
}

export function NodeList(props: NodeListProps) {
  return (
    <section className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <header className="flex items-center justify-between px-6 py-4">
        <div>
          <p className="text-[12px] font-medium uppercase tracking-[0.18em] text-zinc-400">
            Nodes
          </p>
        </div>
        {props.isLoading ? <Spinner /> : null}
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto px-4 pb-4">
        {props.nodes.length === 0 && !props.isLoading ? (
          <div className="rounded-2xl border border-dashed border-zinc-700 px-4 py-5 text-sm leading-6 text-zinc-400">
            No nodes have been discovered yet.
          </div>
        ) : (
          <div className="space-y-2">
            {props.nodes.map((node) => {
              const isSelected = node.name === props.selectedNodeName
              const status = getNodeStatusView(node)

              return (
                <button
                  key={node.name}
                  type="button"
                  onClick={() => props.onSelectNode(node.name)}
                  className={cn(
                    'w-full rounded-2xl border px-4 py-3 text-left transition',
                    isSelected
                      ? 'border-zinc-100 bg-zinc-900'
                      : 'border-zinc-800 bg-zinc-950/40 hover:border-zinc-600',
                  )}
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="space-y-1">
                      <p className="text-sm font-medium text-white">{node.name}</p>
                      <p className="text-xs text-zinc-500">
                        {node.tailscale_ip || 'IP pending'}
                      </p>
                    </div>
                    <StatusDot
                      tone={status.tone}
                      label={status.label}
                      surface="dark"
                      emphasize={isSelected}
                    />
                  </div>
                  <div className="mt-3 flex items-center justify-between gap-3 text-xs text-zinc-400">
                    <span>Last seen {formatRelativeTime(node.last_seen_at)}</span>
                  </div>
                </button>
              )
            })}
          </div>
        )}
      </div>
    </section>
  )
}
