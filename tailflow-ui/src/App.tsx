import { useCallback, useEffect, useMemo, useState } from 'react'
import { useHealth } from './hooks/useHealth'
import { useTopology } from './hooks/useTopology'
import { useUiStore } from './store/ui'
import { useTopologyStream } from './sse/useTopologyStream'
import { isStale } from './lib/stale'
import { Sidebar } from './components/sidebar/Sidebar'
import { EmptyCanvas } from './components/canvas/EmptyCanvas'
import { LiveUpdatesBanner } from './components/canvas/LiveUpdatesBanner'
import { LoadingCanvas } from './components/canvas/LoadingCanvas'
import { TopologyCanvas } from './components/canvas/TopologyCanvas'
import { DetailPanel } from './components/detail/DetailPanel'
import { StatusDot } from './components/shared/StatusDot'
import { ErrorBoundary } from './components/shared/ErrorBoundary'
import { ErrorState } from './components/shared/ErrorState'
import { useTopologyStore } from './store/topology'
import {
  buildTopologyGraphLinks,
  filterVisibleTopologyGraphLinks,
  isLocalTopologyGraphLink,
} from './lib/topology'
import { cn } from './lib/utils'
import { useRenderLoopGuard } from './lib/debug'
import type { NodeResponse } from './api/types'

interface StreamDisplayState {
  status: 'connecting' | 'open' | 'reconnecting' | 'closed'
  error: string | null
  lastEventName: string | null
}

const INITIAL_STREAM_STATE: StreamDisplayState = {
  status: 'connecting',
  error: null,
  lastEventName: null,
}

function areStreamStatesEqual(
  left: StreamDisplayState,
  right: StreamDisplayState,
): boolean {
  return (
    left.status === right.status &&
    left.error === right.error &&
    left.lastEventName === right.lastEventName
  )
}

export default function App() {
  const selectedNodeName = useUiStore((state) => state.selectedNodeName)
  const isDetailPanelOpen = useUiStore((state) => state.isDetailPanelOpen)
  const setSelectedNodeName = useUiStore((state) => state.setSelectedNodeName)
  const closeDetailPanel = useUiStore((state) => state.closeDetailPanel)
  const topologySnapshot = useTopologyStore((state) => state.topologySnapshot)
  const lastAppliedEventName = useTopologyStore(
    (state) => state.lastAppliedEventName,
  )

  const topologyQuery = useTopology()
  const healthQuery = useHealth()
  const [topologyStream, setTopologyStream] = useState<StreamDisplayState>(INITIAL_STREAM_STATE)
  const handleTopologyStreamChange = useCallback((nextState: StreamDisplayState) => {
    setTopologyStream((currentState) =>
      areStreamStatesEqual(currentState, nextState) ? currentState : nextState,
    )
  }, [])

  const health = healthQuery.data ?? null
  const topology = topologySnapshot ?? topologyQuery.data ?? null
  const nodes = useMemo<NodeResponse[]>(
    () =>
      (topology?.nodes ?? []).map((node) => ({
        name: node.name,
        tailscale_ip: node.tailscale_ip,
        online: node.online,
        degraded: node.degraded ?? false,
        collector_degraded: node.collector_degraded,
        workload_degraded: node.workload_degraded,
        last_seen_at: node.last_seen_at,
        collector_error: node.collector_error,
        workload_issues: node.workload_issues,
        snapshot: {
          collected_at: node.last_seen_at,
          port_count: node.ports.length,
          container_count: node.containers.length,
          service_count: node.services.length,
          forward_count: 0,
        },
      })),
    [topology],
  )
  const inventoryNodesByName = useMemo(
    () => Object.fromEntries(nodes.map((node) => [node.name, node])),
    [nodes],
  )

  const selectedNode = useMemo(
    () => nodes.find((node) => node.name === selectedNodeName) ?? null,
    [nodes, selectedNodeName],
  )
  const selectedTopologyNode = useMemo(
    () =>
      topology?.nodes.find((node) => node.name === selectedNodeName) ?? null,
    [selectedNodeName, topology?.nodes],
  )
  const topologyGraphLinks = useMemo(
    () => buildTopologyGraphLinks(topology),
    [topology],
  )
  const visibleTopologyLinks = useMemo(
    () => filterVisibleTopologyGraphLinks(topologyGraphLinks),
    [topologyGraphLinks],
  )
  const selectedInboundRoutes = useMemo(
    () =>
      visibleTopologyLinks.filter(
        (link) =>
          link.to_node === selectedNodeName &&
          link.from_node !== selectedNodeName,
      ),
    [selectedNodeName, visibleTopologyLinks],
  )
  const selectedLocalRoutes = useMemo(
    () =>
      visibleTopologyLinks.filter(
        (link) =>
          isLocalTopologyGraphLink(link) &&
          link.from_node === selectedNodeName &&
          link.to_node === selectedNodeName,
      ),
    [selectedNodeName, visibleTopologyLinks],
  )
  const selectedOutboundRoutes = useMemo(
    () =>
      visibleTopologyLinks.filter(
        (link) =>
          link.from_node === selectedNodeName &&
          link.to_node !== selectedNodeName,
      ),
    [selectedNodeName, visibleTopologyLinks],
  )
  const isLiveUpdatesPaused =
    topologyStream.status === 'reconnecting' ||
    topologyStream.status === 'closed'
  const liveUpdatesMessage =
    topologyStream.status === 'closed'
      ? 'Live updates paused — stream connection closed.'
      : 'Live updates paused — reconnecting...'

  const staleNodeCount = useMemo(
    () => nodes.filter((node) => isStale(node.last_seen_at)).length,
    [nodes],
  )
  const onlineNodeCount = useMemo(
    () => nodes.filter((node) => node.online).length,
    [nodes],
  )
  const topologyNodeCount = useMemo(
    () => topology?.nodes.length ?? 0,
    [topology],
  )
  const topologyEdgeCount = useMemo(
    () => visibleTopologyLinks.length,
    [visibleTopologyLinks],
  )
  const trackedPortNodeCount = useMemo(
    () => nodes.filter((node) => (node.snapshot?.port_count ?? 0) > 0).length,
    [nodes],
  )
  const isDetailPanelVisible =
    isDetailPanelOpen && selectedNodeName !== null

  useRenderLoopGuard('App', {
    selectedNodeName,
    isDetailPanelOpen,
    isDetailPanelVisible,
    topologyStreamStatus: topologyStream.status,
    topologyStreamError: topologyStream.error,
    topologyStreamEvent: topologyStream.lastEventName,
    nodesLength: nodes.length,
    healthStatus: health?.status ?? null,
    topologyVersion: topology?.version ?? null,
    topologyUpdatedAt: topology?.updated_at ?? null,
    topologyNodeCount,
    topologyEdgeCount,
    trackedPortNodeCount,
    staleNodeCount,
    onlineNodeCount,
    storeLastAppliedEventName: lastAppliedEventName,
    topologySnapshotRef: topologySnapshot,
  })

  return (
    <div className="flex h-screen max-h-screen overflow-hidden bg-canvas text-zinc-950">
      <ErrorBoundary
        fallback={
          <SseBoundaryFallback
            onTopologyStreamChange={handleTopologyStreamChange}
          />
        }
      >
        <SseBridge
          onTopologyStreamChange={handleTopologyStreamChange}
        />
      </ErrorBoundary>

      <Sidebar
        health={health}
        isHealthLoading={healthQuery.isPending}
        nodes={nodes}
        isNodesLoading={topologyQuery.isPending && !topology}
        selectedNodeName={selectedNode?.name ?? null}
        onSelectNode={setSelectedNodeName}
      />

      <main className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
        <header className="border-b border-zinc-200 bg-white px-6 py-4">
      
            <div className="flex items-center justify-between gap-3">
              <div className="flex tems-center gap-3">
              <StreamStatus
                label="Topology"
                status={topologyStream.status}
              />
              </div>
              <StoreStatus
                topologyNodeCount={topologyNodeCount}
                topologyEdgeCount={topologyEdgeCount}
                trackedPortNodeCount={trackedPortNodeCount}
                lastAppliedEventName={lastAppliedEventName}
              />
            </div>

        </header>

        <div className="relative flex min-h-0 flex-1 gap-6 overflow-hidden p-6">
          <section
            className={cn(
              'flex min-h-0 flex-1 flex-col overflow-hidden rounded-2xl border border-zinc-200 bg-white transition-[margin] duration-200',
              isDetailPanelVisible ? 'xl:mr-[25.5rem]' : undefined,
            )}
          >
            {topologyQuery.isError && !topology ? (
              <ErrorState
                title="Topology failed to load"
                description="The canvas is wired, but the backend request for /api/v1/topology did not succeed."
                details={topologyQuery.error.message}
              />
            ) : topologyQuery.isPending && !topology ? (
              <LoadingCanvas />
            ) : !topology || topology.nodes.length === 0 ? (
              <EmptyCanvas
                title="No topology data yet."
                description="Tailflow has not reported a graph yet. The backend is still bootstrapping or waiting for node activity."
                onCollect={() => undefined}
                isCollecting={false}
              />
            ) : (
              <div className="flex h-full flex-col">
                {isLiveUpdatesPaused ? (
                  <LiveUpdatesBanner message={liveUpdatesMessage} />
                ) : null}
                <div className="min-h-0 flex-1">
                  <ErrorBoundary
                    fallback={
                      <ErrorState
                        title="Graph rendering failed."
                        description="The canvas hit a rendering error. Refresh the page to retry; the sidebar and detail panel remain available."
                      />
                    }
                  >
                    <TopologyCanvas
                      topology={topology}
                      inventoryNodesByName={inventoryNodesByName}
                      selectedNodeName={selectedNodeName}
                      onSelectNode={setSelectedNodeName}
                    />
                  </ErrorBoundary>
                </div>
              </div>
            )}
          </section>

          <DetailPanel
            selectedNodeName={selectedNodeName}
            isOpen={isDetailPanelOpen}
            inventoryNode={selectedNode}
            topologyNode={selectedTopologyNode}
            inboundRoutes={selectedInboundRoutes}
            localRoutes={selectedLocalRoutes}
            outboundRoutes={selectedOutboundRoutes}
            onClose={closeDetailPanel}
          />
        </div>
      </main>
    </div>
  )
}

function SseBridge({
  onTopologyStreamChange,
}: {
  onTopologyStreamChange: (stream: StreamDisplayState) => void
}) {
  useRenderLoopGuard('SseBridge')

  const topologyStream = useTopologyStream()

  useEffect(() => {
    onTopologyStreamChange({
      status: topologyStream.status,
      error: topologyStream.error,
      lastEventName: topologyStream.lastEventName,
    })
  }, [
    onTopologyStreamChange,
    topologyStream.error,
    topologyStream.lastEventName,
    topologyStream.status,
  ])

  return null
}

function SseBoundaryFallback({
  onTopologyStreamChange,
}: {
  onTopologyStreamChange: (stream: StreamDisplayState) => void
}) {
  useRenderLoopGuard('SseBoundaryFallback')

  useEffect(() => {
    onTopologyStreamChange({
      status: 'closed',
      error: 'SSE topology stream crashed.',
      lastEventName: null,
    })
  }, [onTopologyStreamChange])

  return null
}

function StreamStatus(props: {
  label: string
  status: 'connecting' | 'open' | 'reconnecting' | 'closed'
}) {
  const tone =
    props.status === 'open'
      ? 'online'
      : props.status === 'closed'
        ? 'offline'
        : 'warning'

  return (
    <div className="inline-flex items-center gap-2 rounded-full border border-zinc-200 bg-canvas px-3 py-2">
      <StatusDot tone={tone} label={props.label} />
      <span className="text-xs uppercase tracking-[0.16em] text-zinc-500">
        {props.status}
      </span>
      
    </div>
  )
}

function StoreStatus(props: {
  topologyNodeCount: number
  topologyEdgeCount: number
  trackedPortNodeCount: number
  lastAppliedEventName: string | null
}) {
  return (
    <div className="inline-flex items-center gap-3 rounded-full border border-zinc-200 bg-canvas px-3 py-2">
      <span className="text-xs font-medium uppercase tracking-[0.16em] text-zinc-500">
        Store
      </span>
      <span className="text-xs text-zinc-600">
        {props.topologyNodeCount} nodes
      </span>
      <span className="text-xs text-zinc-600">
        {props.topologyEdgeCount} routes
      </span>
      <span className="text-xs text-zinc-600">
        {props.trackedPortNodeCount} port sets
      </span>
      {props.lastAppliedEventName ? (
        <span className="text-xs text-zinc-500">{props.lastAppliedEventName}</span>
      ) : null}
    </div>
  )
}
