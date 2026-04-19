import { useCallback, useEffect, useMemo, useState } from 'react'
import { useHealth } from './hooks/useHealth'
import { useNodes } from './hooks/useNodes'
import { useRuns } from './hooks/useRuns'
import { useTopology } from './hooks/useTopology'
import { useTriggerRun } from './hooks/useTriggerRun'
import { useUiStore } from './store/ui'
import { useNodeStream } from './sse/useNodeStream'
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
import { filterVisibleTopologyEdges, isSelfTopologyEdge } from './lib/topology'
import { cn } from './lib/utils'
import { useRenderLoopGuard } from './lib/debug'

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
  const portsByNode = useTopologyStore((state) => state.portsByNode)
  const nodeStatusByNode = useTopologyStore((state) => state.nodeStatusByNode)
  const topologySnapshot = useTopologyStore((state) => state.topologySnapshot)
  const lastAppliedEventName = useTopologyStore(
    (state) => state.lastAppliedEventName,
  )

  const nodesQuery = useNodes()
  const runsQuery = useRuns()
  const topologyQuery = useTopology()
  const healthQuery = useHealth()
  const triggerRunMutation = useTriggerRun()
  const [nodeStream, setNodeStream] = useState<StreamDisplayState>(INITIAL_STREAM_STATE)
  const [topologyStream, setTopologyStream] = useState<StreamDisplayState>(INITIAL_STREAM_STATE)
  const handleNodeStreamChange = useCallback((nextState: StreamDisplayState) => {
    setNodeStream((currentState) =>
      areStreamStatesEqual(currentState, nextState) ? currentState : nextState,
    )
  }, [])
  const handleTopologyStreamChange = useCallback((nextState: StreamDisplayState) => {
    setTopologyStream((currentState) =>
      areStreamStatesEqual(currentState, nextState) ? currentState : nextState,
    )
  }, [])

  const nodes = useMemo(
    () =>
      (nodesQuery.data ?? []).map((node) => {
        const status = nodeStatusByNode[node.name]
        const ports = portsByNode[node.name]

        return {
          ...node,
          online: status?.online ?? node.online,
          degraded: status?.degraded ?? node.degraded,
          last_seen_at: status?.last_seen_at ?? node.last_seen_at,
          snapshot: node.snapshot
            ? {
                ...node.snapshot,
                port_count: ports?.length ?? node.snapshot.port_count,
              }
            : ports
              ? {
                  collected_at: status?.last_seen_at ?? node.last_seen_at,
                  port_count: ports.length,
                  container_count: 0,
                  service_count: 0,
                  forward_count: 0,
                }
              : undefined,
        }
      }),
    [nodeStatusByNode, nodesQuery.data, portsByNode],
  )
  const latestRun = useMemo(
    () => runsQuery.data?.[0] ?? null,
    [runsQuery.data],
  )
  const health = healthQuery.data ?? null
  const topology = topologySnapshot ?? topologyQuery.data ?? null
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
  const visibleTopologyEdges = useMemo(
    () => filterVisibleTopologyEdges(topology?.edges),
    [topology?.edges],
  )
  const localTopologyEdges = useMemo(
    () => (topology?.edges ?? []).filter(isSelfTopologyEdge),
    [topology?.edges],
  )
  const selectedInboundEdges = useMemo(
    () =>
      visibleTopologyEdges.filter((edge) => edge.to_node === selectedNodeName),
    [selectedNodeName, visibleTopologyEdges],
  )
  const selectedOutboundEdges = useMemo(
    () =>
      visibleTopologyEdges.filter((edge) => edge.from_node === selectedNodeName),
    [selectedNodeName, visibleTopologyEdges],
  )
  const isLiveUpdatesPaused =
    nodeStream.status === 'reconnecting' ||
    topologyStream.status === 'reconnecting' ||
    nodeStream.status === 'closed' ||
    topologyStream.status === 'closed'
  const liveUpdatesMessage =
    nodeStream.status === 'closed' || topologyStream.status === 'closed'
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
    () => visibleTopologyEdges.length,
    [visibleTopologyEdges],
  )
  const trackedPortNodeCount = useMemo(
    () => Object.keys(portsByNode).length,
    [portsByNode],
  )
  const isDetailPanelVisible =
    isDetailPanelOpen && selectedNodeName !== null

  useRenderLoopGuard('App', {
    selectedNodeName,
    isDetailPanelOpen,
    isDetailPanelVisible,
    nodeStreamStatus: nodeStream.status,
    nodeStreamError: nodeStream.error,
    nodeStreamEvent: nodeStream.lastEventName,
    topologyStreamStatus: topologyStream.status,
    topologyStreamError: topologyStream.error,
    topologyStreamEvent: topologyStream.lastEventName,
    nodesLength: nodes.length,
    latestRunID: latestRun?.id ?? null,
    latestRunFinishedAt: latestRun?.finished_at ?? null,
    latestRunErrorCount: latestRun?.error_count ?? null,
    healthStatus: health?.status ?? null,
    topologyRunId: topology?.run_id ?? null,
    topologyUpdatedAt: topology?.updated_at ?? null,
    topologyNodeCount,
    topologyEdgeCount,
    trackedPortNodeCount,
    staleNodeCount,
    onlineNodeCount,
    storeLastAppliedEventName: lastAppliedEventName,
    portsByNodeRef: portsByNode,
    nodeStatusByNodeRef: nodeStatusByNode,
    topologySnapshotRef: topologySnapshot,
  })

  return (
    <div className="flex h-screen max-h-screen overflow-hidden bg-canvas text-zinc-950">
      <ErrorBoundary
        fallback={
          <SseBoundaryFallback
            onNodeStreamChange={handleNodeStreamChange}
            onTopologyStreamChange={handleTopologyStreamChange}
          />
        }
      >
        <SseBridge
          onNodeStreamChange={handleNodeStreamChange}
          onTopologyStreamChange={handleTopologyStreamChange}
        />
      </ErrorBoundary>

      <Sidebar
        health={health}
        isHealthLoading={healthQuery.isPending}
        nodes={nodes}
        isNodesLoading={nodesQuery.isPending}
        latestRun={latestRun}
        isRunsLoading={runsQuery.isPending}
        selectedNodeName={selectedNode?.name ?? null}
        onSelectNode={setSelectedNodeName}
        onTriggerRun={() => triggerRunMutation.mutate()}
        isTriggeringRun={triggerRunMutation.isPending}
      />

      <main className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
        <header className="border-b border-zinc-200 bg-white px-6 py-4">
      
            <div className="flex flex-wrap items-center justify-end gap-3">
              <StreamStatus
                label="Nodes"
                status={nodeStream.status}
                lastEventName={nodeStream.lastEventName}
              />
              <StreamStatus
                label="Topology"
                status={topologyStream.status}
                lastEventName={topologyStream.lastEventName}
              />
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
                description="Tailflow has not reported a graph yet. Trigger a collection run once the backend is connected to your tailnet."
                onCollect={() => triggerRunMutation.mutate()}
                isCollecting={triggerRunMutation.isPending}
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
                      onCollectNow={() => triggerRunMutation.mutate()}
                      isCollecting={triggerRunMutation.isPending}
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
            inboundEdges={selectedInboundEdges}
            localEdges={localTopologyEdges.filter(
              (edge) => edge.from_node === selectedNodeName,
            )}
            outboundEdges={selectedOutboundEdges}
            onClose={closeDetailPanel}
          />
        </div>
      </main>
    </div>
  )
}

function SseBridge({
  onNodeStreamChange,
  onTopologyStreamChange,
}: {
  onNodeStreamChange: (stream: StreamDisplayState) => void
  onTopologyStreamChange: (stream: StreamDisplayState) => void
}) {
  useRenderLoopGuard('SseBridge')

  const nodeStream = useNodeStream()
  const topologyStream = useTopologyStream()

  useEffect(() => {
    onNodeStreamChange({
      status: nodeStream.status,
      error: nodeStream.error,
      lastEventName: nodeStream.lastEventName,
    })
  }, [
    nodeStream.error,
    nodeStream.lastEventName,
    nodeStream.status,
    onNodeStreamChange,
  ])

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
  onNodeStreamChange,
  onTopologyStreamChange,
}: {
  onNodeStreamChange: (stream: StreamDisplayState) => void
  onTopologyStreamChange: (stream: StreamDisplayState) => void
}) {
  useRenderLoopGuard('SseBoundaryFallback')

  useEffect(() => {
    onNodeStreamChange({
      status: 'closed',
      error: 'SSE node stream crashed.',
      lastEventName: null,
    })
    onTopologyStreamChange({
      status: 'closed',
      error: 'SSE topology stream crashed.',
      lastEventName: null,
    })
  }, [onNodeStreamChange, onTopologyStreamChange])

  return null
}

function StreamStatus(props: {
  label: string
  status: 'connecting' | 'open' | 'reconnecting' | 'closed'
  lastEventName: string | null
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
      {props.lastEventName ? (
        <span className="text-xs text-zinc-500">{props.lastEventName}</span>
      ) : null}
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
        {props.topologyEdgeCount} edges
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
