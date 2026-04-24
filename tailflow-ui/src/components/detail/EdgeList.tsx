import { useState } from 'react'
import type { TopologyGraphLink } from '../../lib/topology'
import {
  formatTopologyEdgePortLabel,
  formatTopologyGraphLinkLabel,
} from '../../lib/topology'

interface EdgeListProps {
  inboundLinks: TopologyGraphLink[]
  localLinks: TopologyGraphLink[]
  outboundLinks: TopologyGraphLink[]
}

export function EdgeList(props: EdgeListProps) {
  return (
    <section className="space-y-4">
      <div>
        <p className="mt-1 text-sm text-zinc-600">
          Request routes involving the selected node.
        </p>
      </div>

      <EdgeSection
        title="Outbound"
        emptyMessage="No outbound routes from this node."
        edges={props.outboundLinks}
        direction="outbound"
      />
      <EdgeSection
        title="Inbound"
        emptyMessage="No inbound routes to this node."
        edges={props.inboundLinks}
        direction="inbound"
      />
      <EdgeSection
        title="Local"
        emptyMessage="No local routes for this node."
        edges={props.localLinks}
        direction="local"
      />
    </section>
  )
}

function EdgeSection(props: {
  title: string
  emptyMessage: string
  edges: TopologyGraphLink[]
  direction: 'inbound' | 'outbound' | 'local'
}) {
  const [isOpen, setIsOpen] = useState(props.edges.length > 0)

  return (
    <div className="rounded-2xl border border-zinc-200 bg-white">
      {/* Header — always visible */}
      <div className="flex items-center gap-3 px-4 py-3">
        <button
          type="button"
          onClick={() => setIsOpen((v) => !v)}
          className="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-md border border-zinc-200 bg-zinc-50 text-zinc-500 transition-colors hover:bg-zinc-100 hover:text-zinc-700"
          aria-label={isOpen ? 'Collapse section' : 'Expand section'}
        >
          <svg
            className={`h-3 w-3 transition-transform duration-200 ${isOpen ? 'rotate-0' : '-rotate-90'}`}
            viewBox="0 0 12 12"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <polyline points="2 4 6 8 10 4" />
          </svg>
        </button>
        <p className="flex-1 text-sm font-medium text-zinc-950">{props.title}</p>
        <span className="text-xs text-zinc-500">{props.edges.length}</span>
      </div>

      {/* Collapsible body */}
      {isOpen && (
        <div className="space-y-3 border-t border-zinc-100 px-4 pb-4 pt-3">
          {props.edges.length === 0 ? (
            <p className="text-sm text-zinc-500">{props.emptyMessage}</p>
          ) : (
            <div className="space-y-3">
              {props.edges.map((edge) => (
                <EdgeCard key={edge.id} edge={edge} direction={props.direction} />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function EdgeCard(props: {
  edge: TopologyGraphLink
  direction: 'inbound' | 'outbound' | 'local'
}) {
  const [isOpen, setIsOpen] = useState(false)

  const { edge } = props

  const title =
    edge.runtime_name ||
    edge.to_name ||
    edge.to_service ||
    edge.to_node ||
    edge.from_name ||
    edge.from_node ||
    edge.input ||
    'unknown'

  const hosts = Array.isArray(edge.hostnames) ? edge.hostnames : []
  const firstHost = hosts[0]

  const fromPort = edge.from_port != null
    ? formatTopologyEdgePortLabel(edge.from_port)
    : '—'
  const toPort = edge.to_port != null
    ? formatTopologyEdgePortLabel(edge.to_port)
    : '—'

  function handleGoTo() {
    if (firstHost) {
      window.open(`http://${firstHost}`, '_blank', 'noopener,noreferrer')
    }
  }

  return (
    <article className="rounded-2xl border border-zinc-100 bg-canvas p-4">
      {/* Clickable header row */}
        <button
          type="button"
          onClick={() => setIsOpen((v) => !v)}
          className="flex w-full items-center overflow-hidden  gap-3  pt-4 text-left"
        >
          <svg
            className={`mt-0.5 h-3.5 w-3.5 flex-shrink-0 text-zinc-400 transition-transform duration-200 ${isOpen ? 'rotate-0' : '-rotate-90'}`}
            viewBox="0 0 12 12"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <polyline points="2 4 6 8 10 4" />
          </svg>
          <div className="min-w-0 flex-1">
            <p className="text-base font-medium text-zinc-950">{title}</p>
            <p className="mt-0.5 break-words text-xs leading-5 text-zinc-500">
              {props.direction === 'outbound' ? `to ${edge.to_node}` : props.direction === 'inbound' ? `from ${edge.from_node}` : edge.to_node}
            </p>
          </div>
        </button>

      {/* Port row — always visible */}
      <div className="mt-3 flex gap-3">
        <div className="flex flex-1 flex-col gap-1 rounded-xl bg-white border border-zinc-100 px-3 py-2">
          <p className="text-[10px] font-semibold uppercase tracking-[0.14em] text-zinc-400">
            From Port
          </p>
          <p className="text-sm font-medium text-zinc-700">{fromPort}</p>
        </div>

        <div className="flex flex-1 flex-col gap-1 rounded-xl bg-white border border-zinc-100 px-3 py-2">
          <p className="text-[10px] font-semibold uppercase tracking-[0.14em] text-zinc-400">
            To Port
          </p>
          <p className="text-sm font-medium text-zinc-700">{toPort}</p>
        </div>
      </div>


      {/* Expanded metadata */}
      {isOpen && (
        <div className="mt-3 space-y-2 text-sm">
          {edge.input && <MetadataRow label="Input" value={edge.input} />}
          {edge.runtime_name && (
            <MetadataRow label="Runtime" value={edge.runtime_name} />
          )}
          {edge.evidence_label && (
            <MetadataRow label="Matched by" value={edge.evidence_label} />
          )}
          {hosts.length > 0 && (
            <MetadataRow label="Hosts" value={hosts.join(', ')} multiline />
          )}
        </div>
      )}

      {/* Footer — always visible */}
      <div className="mt-3 flex flex-1 items-center justify-end border-t border-zinc-300 py-3">
        <button
          type="button"
          onClick={handleGoTo}
          disabled={!firstHost}
          className="flex flex-1 items-center justify-center gap-1.5 rounded-lg border border-zinc-200 bg-white py-2 text-xs font-medium text-zinc-700 shadow-sm transition-colors hover:bg-zinc-50 hover:text-zinc-900 disabled:cursor-not-allowed disabled:opacity-40"
        >
          Go to
          <svg
            className="h-3 w-3"
            viewBox="0 0 12 12"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <path d="M2 10L10 2M10 2H5.5M10 2V6.5" />
          </svg>
        </button>
      </div>
    </article>
  )
}

function MetadataRow(props: {
  label: string
  value: string
  multiline?: boolean
}) {
  return (
    <div className="space-y-1">
      <p className="text-[10px] font-semibold uppercase tracking-[0.14em] text-zinc-400">
        {props.label}
      </p>
      <p
        className={
          props.multiline
            ? 'break-words text-sm leading-5 text-zinc-700'
            : 'text-sm text-zinc-700'
        }
      >
        {props.value}
      </p>
    </div>
  )
}

function formatEndpoint(name: string, node: string, port: number): string {
  return `${name || node || 'unknown'}:${formatTopologyEdgePortLabel(port)}`
}
