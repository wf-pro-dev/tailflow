import { useState } from 'react'
import type { HealthResponse, NodeResponse } from '../../api/types'
import { NodeList } from './NodeList'
import { StatusDot } from '../shared/StatusDot'
import { Spinner } from '../shared/Spinner'
import { formatRelativeTime } from '../../lib/time'

interface SidebarProps {
  health: HealthResponse | null
  isHealthLoading: boolean
  nodes: NodeResponse[]
  isNodesLoading: boolean
  selectedNodeName: string | null
  onSelectNode: (nodeName: string) => void
}

export function Sidebar(props: SidebarProps) {
  const [isHealthCollapsed, setIsHealthCollapsed] = useState(true)
  const healthStatus =
    props.health?.status === 'ok'
      ? { label: 'Healthy', tone: 'online' as const }
      : props.health?.status === 'degraded'
        ? { label: 'Degraded', tone: 'warning' as const }
        : { label: 'Unavailable', tone: 'offline' as const }

  return (
    <aside className="flex h-full w-80 shrink-0 flex-col overflow-hidden border-r border-zinc-800 bg-sidebar text-white">
      <div className="border-b border-zinc-800 px-6 py-6">
        <div className="space-y-3">
          <div className="space-y-1">
            <h1 className="text-xl font-semibold">Tailflow</h1>
            <p className="text-sm leading-6 text-zinc-300">
              is a tool for visualizing the topology of a Tailscale tailnet.
            </p>
          </div>
        </div>

        <div className="mt-6 rounded-2xl border border-zinc-800 bg-zinc-950/60 p-4">
          <div className="flex items-center justify-between gap-3">
            <div className="flex flex-col w-full min-w-0 gap-2">
              <div className="flex items-center justify-between gap-2">
                <span className="text-sm font-medium text-white">Health</span>
                <button
                  type="button"
                  onClick={() => setIsHealthCollapsed((value) => !value)}
                  className="inline-flex h-6 w-6 items-center justify-center rounded-md border border-zinc-800 text-xs text-zinc-400 transition hover:border-zinc-700 hover:text-zinc-200"
                  aria-expanded={!isHealthCollapsed}
                  aria-label={isHealthCollapsed ? 'Expand health card' : 'Collapse health card'}
                >
                  {isHealthCollapsed ? '+' : '-'}
                </button>
              </div>
              <div className="flex">
                {props.isHealthLoading ? (
                  <Spinner />
                ) : (
                  <StatusDot
                    tone={healthStatus.tone}
                    label={healthStatus.label}
                    surface="dark"
                    emphasize
                  />
                )}
              </div>
            </div>

          </div>

          {!isHealthCollapsed ? (
            <dl className="mt-4 space-y-3 text-sm text-zinc-300">
              <HealthRow
                label="Tailnet IP"
                value={props.health?.tailnet_ip || 'Pending'}
                valueClassName="font-mono text-[12px]"
              />
              <HealthRow
                label="Node count"
                value={
                  props.health ? String(props.health.node_count) : String(props.nodes.length)
                }
              />
              <HealthRow
                label="Collector issues"
                value={String(props.health?.collector_degraded_node_count ?? 0)}
              />
              <HealthRow
                label="Workload issues"
                value={String(props.health?.workload_degraded_node_count ?? 0)}
              />
              <HealthRow
                label="Updated"
                value={formatRelativeTime(props.health?.updated_at)}
              />
              <HealthRow
                label="Topology version"
                value={String(props.health?.topology_version ?? 0)}
              />
            </dl>
          ) : null}
        </div>
      </div>

      <NodeList
        nodes={props.nodes}
        isLoading={props.isNodesLoading}
        selectedNodeName={props.selectedNodeName}
        onSelectNode={props.onSelectNode}
      />
    </aside>
  )
}

function HealthRow(props: {
  label: string
  value: string
  valueClassName?: string
}) {
  return (
    <div className="flex items-center justify-between gap-4">
      <dt className="text-zinc-500">{props.label}</dt>
      <dd className={`text-right font-medium text-zinc-100 ${props.valueClassName ?? ''}`}>
        {props.value}
      </dd>
    </div>
  )
}
