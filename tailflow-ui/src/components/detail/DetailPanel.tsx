import { useEffect, useState } from 'react'
import type { NodeResponse, TopologyNodeResponse } from '../../api/types'
import type { TopologyGraphLink } from '../../lib/topology'
import { getNodeStatusView } from '../../lib/node-status'
import { formatRelativeTime, formatTimestamp } from '../../lib/time'
import { isStale } from '../../lib/stale'
import { cn } from '../../lib/utils'
import { PortTable } from './PortTable'
import { ContainerTable } from './ContainerTable'
import { EdgeList } from './EdgeList'
import { ProxyConfigForm } from './ProxyConfigForm'
import { useRenderLoopGuard } from '../../lib/debug'

interface DetailPanelProps {
  selectedNodeName: string | null
  isOpen: boolean
  inventoryNode: NodeResponse | null
  topologyNode: TopologyNodeResponse | null
  inboundRoutes: TopologyGraphLink[]
  localRoutes: TopologyGraphLink[]
  outboundRoutes: TopologyGraphLink[]
  onClose: () => void
}

type DetailTab = 'ports' | 'containers' | 'routes' | 'proxy'

export function DetailPanel(props: DetailPanelProps) {
  useRenderLoopGuard('DetailPanel')

  const isVisible = props.isOpen && props.selectedNodeName !== null
  const [activeTab, setActiveTab] = useState<DetailTab>('ports')

  useEffect(() => {
    if (!isVisible) {
      return
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        props.onClose()
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [isVisible, props.onClose])

  useEffect(() => {
    setActiveTab('ports')
  }, [props.selectedNodeName])

  if (!props.selectedNodeName) {
    return (
      <aside
        aria-hidden="true"
        className="pointer-events-none absolute inset-y-6 right-6 z-20 hidden w-[24rem] translate-x-6 rounded-2xl border border-zinc-200 bg-white opacity-0 transition duration-200 ease-out xl:flex xl:flex-col"
      />
    )
  }

  const status = props.inventoryNode
    ? getNodeStatusView(props.inventoryNode)
    : { label: 'Unknown', tone: 'offline' as const }

  return (
    <aside
      aria-hidden={!isVisible}
      className={cn(
        'absolute inset-y-6 right-6 z-20 hidden w-[24rem] rounded-2xl border border-zinc-200 bg-white transition duration-200 ease-out xl:flex xl:flex-col',
        isVisible
          ? 'pointer-events-auto translate-x-0 opacity-100 shadow-[0_1px_4px_rgba(0,0,0,0.08)]'
          : 'pointer-events-none translate-x-6 opacity-0',
      )}
    >
      <div className="border-b border-zinc-200 px-4 py-3">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <h2 className="truncate text-base font-semibold text-zinc-950">
                {props.selectedNodeName}
              </h2>
              <span
                className={
                  status.tone === 'online'
                    ? 'rounded-full bg-green-50 px-2 py-0.5 text-[11px] font-medium text-green-700'
                    : status.tone === 'warning'
                      ? 'rounded-full bg-amber-50 px-2 py-0.5 text-[11px] font-medium text-amber-700'
                      : 'rounded-full bg-zinc-100 px-2 py-0.5 text-[11px] font-medium text-zinc-600'
                }
              >
                {status.label}
              </span>
            </div>
            <p className="mt-1 truncate text-sm text-zinc-500">
              {props.inventoryNode?.tailscale_ip || props.topologyNode?.tailscale_ip || 'IP pending'}
            </p>
            <div className="mt-2 flex flex-wrap items-center gap-3 text-xs text-zinc-500">
              <span>
                Updated{' '}
                {props.inventoryNode?.last_seen_at
                  ? formatRelativeTime(props.inventoryNode.last_seen_at)
                  : 'unknown'}
              </span>
              <span
                className={
                  props.inventoryNode?.last_seen_at &&
                  isStale(props.inventoryNode.last_seen_at)
                    ? 'text-amber-600'
                    : 'text-zinc-500'
                }
              >
                {status.label}
              </span>
            </div>
          </div>
          <button
            type="button"
            onClick={props.onClose}
            className="rounded-xl border border-zinc-200 px-3 py-2 text-sm font-medium text-zinc-700 transition hover:border-zinc-400 hover:text-zinc-950"
          >
            Close
          </button>
        </div>
      </div>

      <div className="border-b border-zinc-200 px-4 py-3">
        <div className="flex flex-wrap gap-2">
          <TabButton
            label="Ports"
            isActive={activeTab === 'ports'}
            onClick={() => setActiveTab('ports')}
          />
          <TabButton
            label="Containers"
            isActive={activeTab === 'containers'}
            onClick={() => setActiveTab('containers')}
          />
          <TabButton
            label="Routes"
            isActive={activeTab === 'routes'}
            onClick={() => setActiveTab('routes')}
          />
          <TabButton
            label="Proxy"
            isActive={activeTab === 'proxy'}
            onClick={() => setActiveTab('proxy')}
          />
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto p-4">
        {activeTab === 'ports' ? (
          <PortTable ports={props.topologyNode?.ports ?? []} />
        ) : null}
        {activeTab === 'containers' ? (
          <ContainerTable containers={props.topologyNode?.containers ?? []} />
        ) : null}
        {activeTab === 'routes' ? (
          <EdgeList
            inboundLinks={props.inboundRoutes}
            localLinks={props.localRoutes}
            outboundLinks={props.outboundRoutes}
            key={props.selectedNodeName}
          />
        ) : null}
        {activeTab === 'proxy' ? (
          <div className="space-y-4">
            <InspectorRow
              label="Last update"
              value={
                props.inventoryNode?.last_seen_at
                  ? formatTimestamp(props.inventoryNode.last_seen_at)
                  : 'Unknown'
              }
            />
            <ProxyConfigForm nodeName={props.selectedNodeName} />
          </div>
        ) : null}
      </div>
    </aside>
  )
}

function TabButton(props: {
  label: string
  isActive: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={props.onClick}
      className={cn(
        'rounded-full border px-3 py-1.5 text-xs font-medium transition',
        props.isActive
          ? 'border-zinc-950 bg-zinc-950 text-white'
          : 'border-zinc-200 text-zinc-600 hover:border-zinc-400 hover:text-zinc-950',
      )}
    >
      {props.label}
    </button>
  )
}

function InspectorRow(props: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-4 rounded-2xl border border-zinc-200 bg-white px-4 py-3">
      <span className="text-sm text-zinc-500">{props.label}</span>
      <span className="text-right text-sm font-medium text-zinc-950">
        {props.value}
      </span>
    </div>
  )
}
