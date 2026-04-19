import { useState } from 'react'
import type { TopologyEdge } from '../../api/types'
import { formatTopologyEdgeLabel } from '../../lib/topology'
import { Badge } from '../shared/Badge'
import { Tooltip } from '../shared/Tooltip'

interface EdgeListProps {
  inboundEdges: TopologyEdge[]
  localEdges: TopologyEdge[]
  outboundEdges: TopologyEdge[]
}

export function EdgeList(props: EdgeListProps) {
  const [isLocalEdgesOpen, setIsLocalEdgesOpen] = useState(false)

  return (
    <section className="space-y-4">
      <div>
        <p className="mt-1 text-sm text-zinc-600">
          Topology edges involving the selected node.
        </p>
      </div>

      <EdgeSection
        title="Outbound"
        emptyMessage="No outbound edges from this node."
        edges={props.outboundEdges}
        direction="outbound"
      />
      <EdgeSection
        title="Inbound"
        emptyMessage="No inbound edges to this node."
        edges={props.inboundEdges}
        direction="inbound"
      />
      <div className="space-y-3 rounded-2xl border border-zinc-200 bg-white p-4">
        <div className="flex items-start justify-between gap-3">
          <div>
            <p className="text-sm font-medium text-zinc-950">
              Local edges ({props.localEdges.length})
            </p>

          </div>
          <button
            type="button"
            onClick={() => setIsLocalEdgesOpen((current) => !current)}
            className="rounded-full border border-zinc-200 px-3 py-1.5 text-xs font-medium text-zinc-600 transition hover:border-zinc-400 hover:text-zinc-950"
          >
            {isLocalEdgesOpen ? 'Hide' : 'Show'}
          </button>
        </div>
        {isLocalEdgesOpen ? (
          props.localEdges.length === 0 ? (
            <p className="text-sm text-zinc-500">No local edges for this node.</p>
          ) : (
            <div className="space-y-2">
              {props.localEdges.map((edge) => (
                <EdgeCard
                  key={edge.id}
                  edge={edge}
                  direction="outbound"
                  counterpartLabel="same node"
                />
              ))}
            </div>
          )
        ) : null}
      </div>
    </section>
  )
}

function EdgeSection(props: {
  title: string
  emptyMessage: string
  edges: TopologyEdge[]
  direction: 'inbound' | 'outbound'
}) {
  return (
    <div className="space-y-2 rounded-2xl border border-zinc-200 bg-white p-4">
      <p className="text-sm font-medium text-zinc-950">{props.title}</p>
      {props.edges.length === 0 ? (
        <p className="text-sm text-zinc-500">{props.emptyMessage}</p>
      ) : (
        <div className="space-y-2">
          {props.edges.map((edge) => (
            <EdgeCard key={edge.id} edge={edge} direction={props.direction} />
          ))}
        </div>
      )}
    </div>
  )
}

function EdgeCard(props: {
  edge: TopologyEdge
  direction: 'inbound' | 'outbound'
  counterpartLabel?: string
}) {
  const counterpart =
    props.counterpartLabel ??
    (props.direction === 'outbound'
      ? props.edge.to_node ||
      props.edge.to_service ||
      props.edge.to_container ||
      props.edge.raw_upstream ||
      'unresolved'
      : props.edge.from_node ||
      props.edge.from_container ||
      props.edge.raw_upstream ||
      'unknown')
  const counterpartPort =
    props.direction === 'outbound' ? props.edge.to_port : props.edge.from_port
  const edgeKindLabel = props.edge.kind.split('_').join(' ')
  const runtimeDiffers =
    props.direction === 'outbound' &&
    props.edge.to_service &&
    !!props.edge.to_runtime_node &&
    (props.edge.to_runtime_node !== props.edge.to_node ||
      (!!props.edge.to_runtime_container &&
        props.edge.to_runtime_container !== props.edge.to_container))

  return (
    <div className="rounded-xl border border-zinc-100 bg-canvas px-3 py-3">
      <div className="flex items-start justify-between gap-3">
        <div>
          <p className="text-sm font-medium text-zinc-950">{counterpart}</p>
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <Badge
              tone={
                props.edge.kind === 'proxy_pass'
                  ? 'warning'
                  : props.edge.kind === 'service_publish'
                    ? 'online'
                    : 'neutral'
              }
            >
              {edgeKindLabel}
            </Badge>
            <Tooltip
              content={props.edge.raw_upstream || formatTopologyEdgeLabel(props.edge)}
            >
              <span className="text-xs text-zinc-500">
                {formatTopologyEdgeLabel(props.edge)}
              </span>
            </Tooltip>
          </div>
          {props.edge.kind === 'proxy_pass' && (props.edge.to_service || props.edge.to_runtime_node) ? (
            <div className="mt-2 space-y-1 text-xs text-zinc-500">

              {props.edge.to_runtime_container ? (
                runtimeDiffers ? (
                  <p>Service {props.edge.to_service}</p>

                ) : <p className="text-zinc-400"> Running on {props.edge.to_runtime_container} </p>
              ) : (
                <p>Service {props.edge.to_service}</p>
              )}
            </div>
          ) : null}
        </div>
        <Badge className="justify-center">{counterpartPort || 'n/a'}</Badge>
      </div>
    </div>
  )
}
