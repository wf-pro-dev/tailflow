import dagre from '@dagrejs/dagre'
import { Position, type Edge, type Node } from '@xyflow/react'
import type { NodeResponse, TopologyEdge, TopologyNodeResponse, TopologyResponse } from '../../api/types'
import { formatRelativeTime } from '../../lib/time'
import { getNodeStatusView } from '../../lib/node-status'
import { formatTopologyEdgeLabel, isVisibleTopologyEdge } from '../../lib/topology'
import { isStale } from '../../lib/stale'
import { partitionCyclicEdges } from './cycleDetection'

export const TOPOLOGY_NODE_WIDTH = 320
export const TOPOLOGY_NODE_HEIGHT = 224
const TOPOLOGY_NODE_COLUMN_GAP = 220
const TOPOLOGY_NODE_ROW_GAP = 140

export interface TopologyCanvasNodeData extends Record<string, unknown> {
  topologyNode: TopologyNodeResponse
  inventoryNode: NodeResponse | null
  statusLabel: string
  statusTone: 'online' | 'warning' | 'offline'
  lastSeenLabel: string
  isStale: boolean
}

export interface TopologyCanvasEdgeData extends Record<string, unknown> {
  topologyEdge: TopologyEdge
  label: string
  isCyclic: boolean
  isSelfLoop: boolean
  parallelIndex: number
  parallelCount: number
  parallelOffset: number
}

interface BuildCanvasLayoutOptions {
  topology: TopologyResponse
  inventoryNodesByName: Record<string, NodeResponse>
  selectedNodeName: string | null
}

function buildFallbackPosition(index: number): { x: number; y: number } {
  const column = index % 3
  const row = Math.floor(index / 3)

  return {
    x: 40 + column * (TOPOLOGY_NODE_WIDTH + TOPOLOGY_NODE_COLUMN_GAP),
    y: 40 + row * (TOPOLOGY_NODE_HEIGHT + TOPOLOGY_NODE_ROW_GAP),
  }
}

function buildCanvasNode(
  topologyNode: TopologyNodeResponse,
  inventoryNode: NodeResponse | null,
  selectedNodeName: string | null,
): Node<TopologyCanvasNodeData> {
  const fallbackNode = {
    name: topologyNode.name,
    tailscale_ip: topologyNode.tailscale_ip,
    online: topologyNode.online,
    degraded: false,
    last_seen_at: '',
  } satisfies Omit<NodeResponse, 'snapshot'>

  const status = getNodeStatusView(inventoryNode ?? fallbackNode)

  return {
    id: topologyNode.name,
    type: 'topologyNode',
    selected: topologyNode.name === selectedNodeName,
    draggable: false,
    connectable: false,
    data: {
      topologyNode,
      inventoryNode,
      statusLabel: status.label,
      statusTone: status.tone,
      lastSeenLabel: formatRelativeTime(inventoryNode?.last_seen_at),
      isStale: inventoryNode ? isStale(inventoryNode.last_seen_at) : false,
    },
    position: { x: 0, y: 0 },
    sourcePosition: Position.Right,
    targetPosition: Position.Left,
  }
}

function buildCanvasEdge(
  edge: TopologyEdge,
  isCyclic: boolean,
  parallelIndex: number,
  parallelCount: number,
  parallelOffset: number,
): Edge<TopologyCanvasEdgeData> {
  return {
    id: edge.id,
    source: edge.from_node,
    target: edge.to_node,
    type: 'topologyEdge',
    selectable: false,
    markerEnd: 'url(#tailflow-edge-arrow)',
    sourceHandle: edge.from_node === edge.to_node ? 'self-source' : 'default-source',
    targetHandle: edge.from_node === edge.to_node ? 'self-target' : 'default-target',
    data: {
      topologyEdge: edge,
      label: formatTopologyEdgeLabel(edge),
      isCyclic,
      isSelfLoop: edge.from_node === edge.to_node,
      parallelIndex,
      parallelCount,
      parallelOffset,
    },
  }
}

function buildParallelOffsets(count: number): number[] {
  if (count <= 1) {
    return [0]
  }

  const center = (count - 1) / 2
  const spacing = 26

  return Array.from({ length: count }, (_, index) => (index - center) * spacing)
}

function annotateParallelEdges(
  edges: Array<{ edge: TopologyEdge; isCyclic: boolean }>,
): Array<Edge<TopologyCanvasEdgeData>> {
  const groupedEdges = new Map<string, Array<{ edge: TopologyEdge; isCyclic: boolean }>>()

  for (const entry of edges) {
    const key = `${entry.edge.from_node}->${entry.edge.to_node}`
    const currentGroup = groupedEdges.get(key) ?? []
    currentGroup.push(entry)
    groupedEdges.set(key, currentGroup)
  }

  const canvasEdges: Array<Edge<TopologyCanvasEdgeData>> = []

  for (const group of groupedEdges.values()) {
    const offsets = buildParallelOffsets(group.length)
    for (const [index, entry] of group.entries()) {
      canvasEdges.push(
        buildCanvasEdge(
          entry.edge,
          entry.isCyclic,
          index,
          group.length,
          offsets[index] ?? 0,
        ),
      )
    }
  }

  return canvasEdges
}

export function buildCanvasLayout(
  options: BuildCanvasLayoutOptions,
): {
  nodes: Array<Node<TopologyCanvasNodeData>>
  edges: Array<Edge<TopologyCanvasEdgeData>>
} {
  const graph = new dagre.graphlib.Graph()
  graph.setDefaultEdgeLabel(() => ({}))
  graph.setGraph({
    rankdir: 'LR',
    nodesep: TOPOLOGY_NODE_ROW_GAP,
    ranksep: TOPOLOGY_NODE_COLUMN_GAP,
    marginx: 60,
    marginy: 60,
  })

  const nodes = options.topology.nodes.map((topologyNode) =>
    buildCanvasNode(
      topologyNode,
      options.inventoryNodesByName[topologyNode.name] ?? null,
      options.selectedNodeName,
    ),
  )

  for (const node of nodes) {
    graph.setNode(node.id, {
      width: TOPOLOGY_NODE_WIDTH,
      height: TOPOLOGY_NODE_HEIGHT,
    })
  }

  const validNodeIds = new Set(nodes.map((node) => node.id))
  const layoutEdges = options.topology.edges.filter(
    (edge) =>
      isVisibleTopologyEdge(edge) &&
      edge.from_node.length > 0 &&
      edge.to_node.length > 0 &&
      validNodeIds.has(edge.from_node) &&
      validNodeIds.has(edge.to_node),
  )

  const partitionedEdges = partitionCyclicEdges(layoutEdges)
  for (const edge of partitionedEdges.dagreEdges) {
    graph.setEdge(edge.from_node, edge.to_node)
  }

  dagre.layout(graph)

  const positionedNodes = nodes.map((node, index) => {
    const positionedNode = graph.node(node.id)
    const fallbackPosition = buildFallbackPosition(index)
    const hasValidPosition =
      typeof positionedNode?.x === 'number' &&
      Number.isFinite(positionedNode.x) &&
      typeof positionedNode?.y === 'number' &&
      Number.isFinite(positionedNode.y)

    return {
      ...node,
      position: {
        x: hasValidPosition
          ? positionedNode.x - TOPOLOGY_NODE_WIDTH / 2
          : fallbackPosition.x,
        y: hasValidPosition
          ? positionedNode.y - TOPOLOGY_NODE_HEIGHT / 2
          : fallbackPosition.y,
      },
    }
  })

  const edges = annotateParallelEdges([
    ...partitionedEdges.dagreEdges.map((edge) => ({ edge, isCyclic: false })),
    ...partitionedEdges.cyclicEdges.map((edge) => ({ edge, isCyclic: true })),
  ])

  return {
    nodes: positionedNodes,
    edges,
  }
}
