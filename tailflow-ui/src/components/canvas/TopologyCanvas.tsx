import { useDeferredValue, useEffect, useMemo, useRef } from 'react'
import {
  Background,
  BackgroundVariant,
  MarkerType,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
} from '@xyflow/react'
import type { NodeResponse, TopologyResponse } from '../../api/types'
import { EmptyCanvas } from './EmptyCanvas'
import { CanvasControls } from './CanvasControls'
import { buildCanvasLayout } from './layout'
import { TopologyEdge } from './TopologyEdge'
import { TopologyNode } from './TopologyNode'
import { useRenderLoopGuard } from '../../lib/debug'

interface TopologyCanvasProps {
  topology: TopologyResponse | null
  inventoryNodesByName: Record<string, NodeResponse>
  selectedNodeName: string | null
  onSelectNode: (nodeName: string) => void
}

const nodeTypes = {
  topologyNode: TopologyNode,
}

const edgeTypes = {
  topologyEdge: TopologyEdge,
}

function TopologyCanvasInner(props: TopologyCanvasProps) {
  useRenderLoopGuard('TopologyCanvasInner')

  const deferredTopology = useDeferredValue(props.topology)
  const reactFlow = useReactFlow()
  const hasAutoFitOnceRef = useRef(false)

  const canvasLayout = useMemo(() => {
    if (!deferredTopology) {
      return {
        nodes: [],
        edges: [],
      }
    }

    return buildCanvasLayout({
      topology: deferredTopology,
      inventoryNodesByName: props.inventoryNodesByName,
      selectedNodeName: props.selectedNodeName,
    })
  }, [
    deferredTopology,
    props.inventoryNodesByName,
    props.selectedNodeName,
  ])

  const flowEdges = useMemo(
    () =>
      canvasLayout.edges.map((edge) => ({
        ...edge,
        markerEnd: {
          type: MarkerType.ArrowClosed,
          width: 16,
          height: 16,
          color: edge.data?.isCyclic ? '#f59e0b' : '#a1a1aa',
        },
      })),
    [canvasLayout.edges],
  )

  useEffect(() => {
    if (!deferredTopology) {
      return
    }

    console.debug('[tailflow:canvas] layout', {
      nodes: canvasLayout.nodes.length,
      edges: canvasLayout.edges.length,
      version: deferredTopology.version,
      updatedAt: deferredTopology.updated_at,
    })
  }, [canvasLayout.edges.length, canvasLayout.nodes.length, deferredTopology])

  useEffect(() => {
    if (!deferredTopology || canvasLayout.nodes.length === 0) {
      hasAutoFitOnceRef.current = false
      return
    }

    if (hasAutoFitOnceRef.current) {
      return
    }

    hasAutoFitOnceRef.current = true
    void reactFlow.fitView({ padding: 0.16, duration: 250 })
  }, [canvasLayout.nodes.length, deferredTopology, reactFlow])

  if (!deferredTopology || deferredTopology.nodes.length === 0) {
    return (
        <EmptyCanvas
          title="No topology data yet."
          description="Tailflow is waiting for live topology data from the backend."
          onCollect={() => undefined}
          isCollecting={false}
        />
      )
  }

  return (
    <div className="h-full flex-1 bg-canvas">
      <ReactFlow
        nodes={canvasLayout.nodes}
        edges={flowEdges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        minZoom={0.4}
        maxZoom={1.6}
        proOptions={{ hideAttribution: true }}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable
        onNodeClick={(_, node) => {
          props.onSelectNode(node.id)
        }}
      >
        <svg>
          <defs>
            <marker
              id="tailflow-edge-arrow"
              markerWidth="10"
              markerHeight="10"
              refX="9"
              refY="5"
              orient="auto"
              markerUnits="strokeWidth"
            >
              <path d="M0,0 L10,5 L0,10 z" fill="#a1a1aa" />
            </marker>
          </defs>
        </svg>
        <Background
          variant={BackgroundVariant.Dots}
          gap={24}
          size={2}
          color="#e4e4e7"
        />
        <CanvasControls />
      </ReactFlow>
    </div>
  )
}

export function TopologyCanvas(props: TopologyCanvasProps) {
  return (
    <ReactFlowProvider>
      <TopologyCanvasInner {...props} />
    </ReactFlowProvider>
  )
}
