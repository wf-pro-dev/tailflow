import dagre from '@dagrejs/dagre'
import { Position, type Edge, type Node } from '@xyflow/react'
import type {
  NodeResponse,
  TopologyEdge,
  TopologyNodeResponse,
  TopologyResponse,
} from '../../api/types'
import { formatRelativeTime } from '../../lib/time'
import { getNodeStatusView } from '../../lib/node-status'
import {
  buildTopologyEdgeEndpointLabel,
  formatTopologyEdgePortLabel,
  isVisibleTopologyEdge,
  type TopologyEdgeEndpointLabel,
} from '../../lib/topology'
import { isStale } from '../../lib/stale'
import { partitionCyclicEdges } from './cycleDetection'

export const TOPOLOGY_NODE_WIDTH = 320
export const TOPOLOGY_NODE_HEIGHT = 224
const TOPOLOGY_NODE_COLUMN_GAP = 280
const TOPOLOGY_NODE_ROW_GAP = 160

export interface TopologyCanvasHandle extends Record<string, unknown> {
  id: string
  top: number
}

export interface TopologyCanvasNodeData extends Record<string, unknown> {
  topologyNode: TopologyNodeResponse
  inventoryNode: NodeResponse | null
  statusLabel: string
  statusTone: 'online' | 'warning' | 'offline'
  lastSeenLabel: string
  isStale: boolean
  sourceHandles: TopologyCanvasHandle[]
  targetHandles: TopologyCanvasHandle[]
}

export interface TopologyCanvasEdgeData extends Record<string, unknown> {
  topologyEdge: TopologyEdge
  sourceEndpoint: TopologyEdgeEndpointLabel
  targetEndpoint: TopologyEdgeEndpointLabel
  popoverItems: TopologyEdgeEndpointLabel[]
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

interface CollapsedCanvasEdgeGroup {
  representativeEdge: TopologyEdge
  isCyclic: boolean
  popoverItems: TopologyEdgeEndpointLabel[]
}

function buildSourceHandleId(edgeId: string): string {
  return `edge-source:${edgeId}`
}

function buildTargetHandleId(edgeId: string): string {
  return `edge-target:${edgeId}`
}

function buildSharedSourceHandleId(edge: TopologyEdge): string {
  const sourceEndpoint = buildTopologyEdgeEndpointLabel(edge, 'source')

  return buildSourceHandleId(
    `${edge.from_node}:${sourceEndpoint.name}:${sourceEndpoint.portLabel}`,
  )
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
  handles: {
    sourceHandles: TopologyCanvasHandle[]
    targetHandles: TopologyCanvasHandle[]
  },
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
      sourceHandles: handles.sourceHandles,
      targetHandles: handles.targetHandles,
    },
    position: { x: 0, y: 0 },
    sourcePosition: Position.Right,
    targetPosition: Position.Left,
  }
}

function buildCanvasEdge(
  edgeGroup: CollapsedCanvasEdgeGroup,
  parallelIndex: number,
  parallelCount: number,
  parallelOffset: number,
): Edge<TopologyCanvasEdgeData> {
  const edge = edgeGroup.representativeEdge

  return {
    id: edge.id,
    source: edge.from_node,
    target: edge.to_node,
    type: 'topologyEdge',
    selectable: false,
    markerEnd: 'url(#tailflow-edge-arrow)',
    sourceHandle: buildSharedSourceHandleId(edge),
    targetHandle: buildTargetHandleId(edge.id),
    data: {
      topologyEdge: edge,
      sourceEndpoint: buildTopologyEdgeEndpointLabel(edge, 'source'),
      targetEndpoint: buildTopologyEdgeEndpointLabel(edge, 'target'),
      popoverItems: edgeGroup.popoverItems,
      isCyclic: edgeGroup.isCyclic,
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
  edgeGroups: CollapsedCanvasEdgeGroup[],
): Array<Edge<TopologyCanvasEdgeData>> {
  const groupedEdges = new Map<string, CollapsedCanvasEdgeGroup[]>()

  for (const edgeGroup of edgeGroups) {
    const key = `${edgeGroup.representativeEdge.from_node}->${edgeGroup.representativeEdge.to_node}`
    const currentGroup = groupedEdges.get(key) ?? []
    currentGroup.push(edgeGroup)
    groupedEdges.set(key, currentGroup)
  }

  const canvasEdges: Array<Edge<TopologyCanvasEdgeData>> = []

  for (const group of groupedEdges.values()) {
    const offsets = buildParallelOffsets(group.length)

    for (const [index, edgeGroup] of group.entries()) {
      canvasEdges.push(
        buildCanvasEdge(edgeGroup, index, group.length, offsets[index] ?? 0),
      )
    }
  }

  return canvasEdges
}

function compareEdgesForHandle(
  edgeA: TopologyEdge,
  edgeB: TopologyEdge,
  direction: 'source' | 'target',
): number {
  const counterpartA =
    direction === 'source' ? edgeA.to_node : edgeA.from_node
  const counterpartB =
    direction === 'source' ? edgeB.to_node : edgeB.from_node
  const nameComparison = counterpartA.localeCompare(counterpartB)

  if (nameComparison !== 0) {
    return nameComparison
  }

  const portComparison =
    (direction === 'source' ? edgeA.to_port : edgeA.from_port) -
    (direction === 'source' ? edgeB.to_port : edgeB.from_port)

  if (portComparison !== 0) {
    return portComparison
  }

  return edgeA.id.localeCompare(edgeB.id)
}

function buildCollapsedEdgeGroups(
  entries: Array<{ edge: TopologyEdge; isCyclic: boolean }>,
): CollapsedCanvasEdgeGroup[] {
  const groupedEntries = new Map<
    string,
    Array<{ edge: TopologyEdge; isCyclic: boolean }>
  >()

  for (const entry of entries) {
    const key = [
      entry.isCyclic ? 'cyclic' : 'direct',
      buildSharedSourceHandleId(entry.edge),
      entry.edge.to_node,
    ].join('|')
    const currentGroup = groupedEntries.get(key) ?? []
    currentGroup.push(entry)
    groupedEntries.set(key, currentGroup)
  }

  const collapsedGroups: CollapsedCanvasEdgeGroup[] = []

  for (const group of groupedEntries.values()) {
    const sortedGroup = [...group].sort((left, right) => {
      const portComparison = left.edge.to_port - right.edge.to_port

      if (portComparison !== 0) {
        return portComparison
      }

      return left.edge.id.localeCompare(right.edge.id)
    })
    const representativeEntry = sortedGroup[0]

    if (!representativeEntry) {
      continue
    }

    const popoverItems = Array.from(
      new Map(
        sortedGroup.map(({ edge }) => {
          const targetEndpoint = buildTopologyEdgeEndpointLabel(edge, 'target')

          return [
            `${targetEndpoint.name}:${formatTopologyEdgePortLabel(edge.to_port)}`,
            targetEndpoint,
          ]
        }),
      ).values(),
    )

    collapsedGroups.push({
      representativeEdge: representativeEntry.edge,
      isCyclic: representativeEntry.isCyclic,
      popoverItems,
    })
  }

  return collapsedGroups
}

function buildHandleOffsets(count: number): number[] {
  if (count === 0) {
    return []
  }

  if (count === 1) {
    return [TOPOLOGY_NODE_HEIGHT / 2]
  }

  const topPadding = 28
  const usableHeight = TOPOLOGY_NODE_HEIGHT - topPadding * 2

  return Array.from(
    { length: count },
    (_, index) => topPadding + (usableHeight * index) / (count - 1),
  )
}

function buildNodeHandleMap(
  nodeIds: string[],
  edgeGroups: CollapsedCanvasEdgeGroup[],
): Map<string, { sourceHandles: TopologyCanvasHandle[]; targetHandles: TopologyCanvasHandle[] }> {
  const handleMap = new Map<
    string,
    { sourceHandles: TopologyCanvasHandle[]; targetHandles: TopologyCanvasHandle[] }
  >()
  const outboundEdgesByNode = new Map<string, TopologyEdge[]>()
  const inboundEdgesByNode = new Map<string, TopologyEdge[]>()

  for (const nodeId of nodeIds) {
    handleMap.set(nodeId, {
      sourceHandles: [],
      targetHandles: [],
    })
  }

  for (const edgeGroup of edgeGroups) {
    const edge = edgeGroup.representativeEdge
    const outboundEdges = outboundEdgesByNode.get(edge.from_node) ?? []
    outboundEdges.push(edge)
    outboundEdgesByNode.set(edge.from_node, outboundEdges)

    const inboundEdges = inboundEdgesByNode.get(edge.to_node) ?? []
    inboundEdges.push(edge)
    inboundEdgesByNode.set(edge.to_node, inboundEdges)
  }

  for (const nodeId of nodeIds) {
    const sourceEdges = [...(outboundEdgesByNode.get(nodeId) ?? [])].sort(
      (edgeA, edgeB) => compareEdgesForHandle(edgeA, edgeB, 'source'),
    )
    const targetEdges = [...(inboundEdgesByNode.get(nodeId) ?? [])].sort(
      (edgeA, edgeB) => compareEdgesForHandle(edgeA, edgeB, 'target'),
    )

    const uniqueSourceHandleIds = Array.from(
      new Set(sourceEdges.map((edge) => buildSharedSourceHandleId(edge))),
    )
    const sourceOffsets = buildHandleOffsets(uniqueSourceHandleIds.length)
    const targetOffsets = buildHandleOffsets(targetEdges.length)

    handleMap.set(nodeId, {
      sourceHandles: uniqueSourceHandleIds.map((handleId, index) => ({
        id: handleId,
        top: sourceOffsets[index] ?? TOPOLOGY_NODE_HEIGHT / 2,
      })),
      targetHandles: targetEdges.map((edge, index) => ({
        id: buildTargetHandleId(edge.id),
        top: targetOffsets[index] ?? TOPOLOGY_NODE_HEIGHT / 2,
      })),
    })
  }

  return handleMap
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

  const topologyNodeIds = options.topology.nodes.map((node) => node.name)
  const validNodeIds = new Set(topologyNodeIds)
  const layoutEdges = options.topology.edges.filter(
    (edge) =>
      isVisibleTopologyEdge(edge) &&
      edge.from_node.length > 0 &&
      edge.to_node.length > 0 &&
      validNodeIds.has(edge.from_node) &&
      validNodeIds.has(edge.to_node),
  )
  const partitionedEdges = partitionCyclicEdges(layoutEdges)
  const collapsedEdgeGroups = buildCollapsedEdgeGroups([
    ...partitionedEdges.dagreEdges.map((edge) => ({ edge, isCyclic: false })),
    ...partitionedEdges.cyclicEdges.map((edge) => ({ edge, isCyclic: true })),
  ])
  const nodeHandleMap = buildNodeHandleMap(topologyNodeIds, collapsedEdgeGroups)
  const nodes = options.topology.nodes.map((topologyNode) =>
    buildCanvasNode(
      topologyNode,
      options.inventoryNodesByName[topologyNode.name] ?? null,
      options.selectedNodeName,
      nodeHandleMap.get(topologyNode.name) ?? {
        sourceHandles: [],
        targetHandles: [],
      },
    ),
  )

  for (const nodeId of topologyNodeIds) {
    graph.setNode(nodeId, {
      width: TOPOLOGY_NODE_WIDTH,
      height: TOPOLOGY_NODE_HEIGHT,
    })
  }

  for (const edgeGroup of collapsedEdgeGroups.filter(
    (edgeGroup) => !edgeGroup.isCyclic,
  )) {
    graph.setEdge(
      edgeGroup.representativeEdge.from_node,
      edgeGroup.representativeEdge.to_node,
    )
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

  return {
    nodes: positionedNodes,
    edges: annotateParallelEdges(collapsedEdgeGroups),
  }
}
