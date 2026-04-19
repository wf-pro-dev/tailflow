import type { NodeProps } from '@xyflow/react'
import { Handle, Position } from '@xyflow/react'
import type { TopologyCanvasNodeData } from './layout'
import { cn } from '../../lib/utils'

export function TopologyNode(props: NodeProps) {
  const {
    topologyNode,
    inventoryNode,
    statusLabel,
    statusTone,
    lastSeenLabel,
    isStale,
  } =
    props.data as TopologyCanvasNodeData
  const publishedContainerPortCount = topologyNode.containers.reduce(
    (total, container) => total + container.published_ports.length,
    0,
  )

  return (
    <div
      className={cn(
        'flex h-[224px] w-[320px] flex-col rounded-2xl border bg-white text-left transition hover:border-zinc-400 hover:shadow-[0_1px_4px_rgba(0,0,0,0.08)]',
        props.selected
          ? 'border-zinc-950'
          : isStale
            ? 'border-amber-500'
            : 'border-zinc-200',
      )}
    >
      <Handle
        id="default-target"
        type="target"
        position={Position.Left}
        className="!h-2.5 !w-2.5 !border !border-zinc-300 !bg-white"
      />
      <Handle
        id="default-source"
        type="source"
        position={Position.Right}
        className="!h-2.5 !w-2.5 !border !border-zinc-300 !bg-white"
      />
      <Handle
        id="self-source"
        type="source"
        position={Position.Right}
        className="!top-5 !h-2 !w-2 !border !border-zinc-300 !bg-white"
      />
      <Handle
        id="self-target"
        type="target"
        position={Position.Right}
        className="!top-10 !h-2 !w-2 !border !border-zinc-300 !bg-white"
      />

      <div className="flex items-start justify-between gap-4 border-b border-zinc-100 px-4 py-4">
        <div className="min-w-0 space-y-1">
          <h3 className="truncate text-[13px] font-medium text-zinc-950">
            {topologyNode.name}
          </h3>
          <p className="truncate text-[11px] text-zinc-500">
            {topologyNode.tailscale_ip || 'IP pending'}
          </p>
        </div>
        <span
          className={cn(
            'inline-flex shrink-0 items-center gap-2 rounded-full px-2.5 py-1 text-[11px] font-medium',
            statusTone === 'online'
              ? 'bg-green-50 text-green-700'
              : statusTone === 'warning'
                ? 'bg-amber-50 text-amber-700'
                : 'bg-zinc-100 text-zinc-600',
          )}
        >
          <span
            className={cn(
              'h-2 w-2 rounded-full',
              statusTone === 'online'
                ? 'bg-green-500'
                : statusTone === 'warning'
                  ? 'bg-amber-500'
                  : 'bg-zinc-500',
            )}
          />
          {statusLabel}
        </span>
      </div>

      <div className="grid grid-cols-3 gap-3 border-b border-zinc-100 px-4 py-3">
        <MetricCard label="Ports" value={String(topologyNode.ports.length)} />
        <MetricCard
          label="Containers"
          value={String(topologyNode.containers.length)}
        />
        <MetricCard
          label="Publishes"
          value={String(publishedContainerPortCount)}
        />
      </div>

      <div className="flex min-h-0 flex-1 flex-col px-4 py-3">
        <div className="text-[11px] text-zinc-500">
          Last seen {inventoryNode ? lastSeenLabel : 'unknown'}
        </div>
      </div>
    </div>
  )
}

function MetricCard(props: { label: string; value: string }) {
  return (
    <div className="rounded-xl border border-zinc-100 bg-canvas px-3 py-2">
      <p className="text-[11px] uppercase tracking-[0.18em] text-zinc-400">
        {props.label}
      </p>
      <p className="mt-1 text-base font-semibold text-zinc-950">{props.value}</p>
    </div>
  )
}
