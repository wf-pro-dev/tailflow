import { create } from 'zustand'
import type {
  EdgeDiff,
  EdgeEvent,
  ListenPort,
  NodeResponse,
  NodeStatus,
  NodeStatusEvent,
  PortBoundEvent,
  PortReleasedEvent,
  SnapshotEvent,
  TopologyEdge,
  TopologyNodeResponse,
  TopologyResponse,
} from '../api/types'
import {
  normalizeTopologyNode,
  normalizeTopologyResponse,
} from '../lib/topology'

interface TopologyStoreState {
  topologySnapshot: TopologyResponse | null
  portsByNode: Record<string, ListenPort[]>
  nodeStatusByNode: Record<string, NodeStatus>
  lastEdgeDiff: EdgeDiff | null
  lastAppliedEventName: string | null
  applyNodesSnapshot: (nodes: NodeResponse[]) => void
  applySnapshot: (snapshot: TopologyResponse) => void
  applySnapshotUpdated: (event: SnapshotEvent) => void
  applyPortBound: (event: PortBoundEvent) => void
  applyPortReleased: (event: PortReleasedEvent) => void
  applyNodeStatus: (eventName: string, event: NodeStatusEvent) => void
  applyEdgeDiff: (eventName: string, event: EdgeEvent) => void
  reset: () => void
}

export interface TopologyStoreSummary {
  topologyNodeCount: number
  topologyEdgeCount: number
  trackedPortNodeCount: number
  trackedStatusNodeCount: number
  lastAppliedEventName: string | null
  lastEdgeDiffCounts: {
    added: number
    removed: number
    changed: number
  }
}

function portKey(port: ListenPort): string {
  return [
    port.addr,
    String(port.port),
    port.proto,
    String(port.pid),
    port.process,
  ].join('|')
}

function edgeKey(edge: TopologyEdge): string {
  return [
    edge.from_node,
    String(edge.from_port),
    edge.from_process,
    edge.from_container,
    edge.to_service,
    edge.kind,
    edge.raw_upstream,
  ].join('|')
}

function sortPorts(ports: ListenPort[]): ListenPort[] {
  return [...ports].sort((left, right) => {
    if (left.port !== right.port) {
      return left.port - right.port
    }
    if (left.proto !== right.proto) {
      return left.proto.localeCompare(right.proto)
    }
    if (left.addr !== right.addr) {
      return left.addr.localeCompare(right.addr)
    }
    return left.process.localeCompare(right.process)
  })
}

function sortEdges(edges: TopologyEdge[]): TopologyEdge[] {
  return [...edges].sort((left, right) => {
    if (left.from_node !== right.from_node) {
      return left.from_node.localeCompare(right.from_node)
    }
    if (left.from_port !== right.from_port) {
      return left.from_port - right.from_port
    }
    if (left.kind !== right.kind) {
      return left.kind.localeCompare(right.kind)
    }
    return left.raw_upstream.localeCompare(right.raw_upstream)
  })
}

function upsertPort(ports: ListenPort[], nextPort: ListenPort): ListenPort[] {
  const nextPortKey = portKey(nextPort)
  const filteredPorts = ports.filter((port) => portKey(port) !== nextPortKey)
  return sortPorts([...filteredPorts, nextPort])
}

function removePort(ports: ListenPort[], removedPort: ListenPort): ListenPort[] {
  return sortPorts(ports.filter((port) => portKey(port) !== portKey(removedPort)))
}

function replaceTopologyNode(
  topologySnapshot: TopologyResponse | null,
  nodeName: string,
  updater: (node: TopologyNodeResponse) => TopologyNodeResponse,
): TopologyResponse | null {
  if (!topologySnapshot) {
    return null
  }

  return {
    ...topologySnapshot,
    nodes: topologySnapshot.nodes.map((node) =>
      node.name === nodeName ? updater(node) : node,
    ),
  }
}

function applyEdgeDiffToSnapshot(
  topologySnapshot: TopologyResponse | null,
  event: EdgeEvent,
): TopologyResponse | null {
  if (!topologySnapshot) {
    return null
  }

  const edgeMap = new Map(
    topologySnapshot.edges.map((edge) => [edgeKey(edge), edge] as const),
  )

  for (const removedEdge of event.diff.removed) {
    edgeMap.delete(edgeKey(removedEdge))
  }
  for (const changedEdge of event.diff.changed) {
    edgeMap.set(edgeKey(changedEdge), changedEdge)
  }
  for (const addedEdge of event.diff.added) {
    edgeMap.set(edgeKey(addedEdge), addedEdge)
  }

  return {
    ...topologySnapshot,
    edges: sortEdges(
      [...edgeMap.values()].filter((edge) => edge.kind !== 'service_publish'),
    ),
  }
}

function buildNodeStatusByNode(nodes: NodeResponse[]): Record<string, NodeStatus> {
  return Object.fromEntries(
    nodes.map((node) => [
      node.name,
      {
        node_name: node.name,
        online: node.online,
        degraded: node.degraded,
        last_seen_at: node.last_seen_at,
      },
    ]),
  )
}

function buildPortsByNode(snapshot: TopologyResponse): Record<string, ListenPort[]> {
  return Object.fromEntries(
    snapshot.nodes.map((node) => [node.name, sortPorts(node.ports)]),
  )
}

function logTopologyStoreState(actionName: string, state: TopologyStoreState) {
  console.debug(`[tailflow:store] ${actionName}`, selectTopologyStoreSummary(state))
}

export const useTopologyStore = create<TopologyStoreState>((set, get) => ({
  topologySnapshot: null,
  portsByNode: {},
  nodeStatusByNode: {},
  lastEdgeDiff: null,
  lastAppliedEventName: null,

  applyNodesSnapshot: (nodes) => {
    set((state) => ({
      ...state,
      nodeStatusByNode: buildNodeStatusByNode(nodes),
      lastAppliedEventName: 'nodes.snapshot',
    }))
    logTopologyStoreState('nodes.snapshot', get())
  },

  applySnapshot: (snapshot) => {
    const normalizedSnapshot = normalizeTopologyResponse(snapshot)
    set((state) => ({
      ...state,
      topologySnapshot: {
        ...normalizedSnapshot,
        edges: sortEdges(normalizedSnapshot.edges),
        nodes: normalizedSnapshot.nodes.map((node) => ({
          ...node,
          ports: sortPorts(node.ports),
        })),
      },
      portsByNode: buildPortsByNode(normalizedSnapshot),
      lastEdgeDiff: null,
      lastAppliedEventName: 'topology.snapshot',
    }))
    logTopologyStoreState('topology.snapshot', get())
  },

  applySnapshotUpdated: (event) => {
    set((state) => {
      const nodeName = event.snapshot.node_name
      const normalizedNode = normalizeTopologyNode({
        name: event.snapshot.node_name,
        tailscale_ip: event.snapshot.tailscale_ip,
        online: true,
        ports: event.snapshot.ports,
        containers: event.snapshot.containers,
        services: event.snapshot.services,
      })
      const nextNodeStatus: NodeStatus = {
        node_name: nodeName,
        online: true,
        degraded: state.nodeStatusByNode[nodeName]?.degraded ?? false,
        last_seen_at: event.snapshot.collected_at,
        last_error: event.snapshot.error,
      }

      return {
        ...state,
        portsByNode: {
          ...state.portsByNode,
          [nodeName]: sortPorts(normalizedNode.ports),
        },
        nodeStatusByNode: {
          ...state.nodeStatusByNode,
          [nodeName]: nextNodeStatus,
        },
        topologySnapshot: replaceTopologyNode(
          state.topologySnapshot,
          nodeName,
          (node) => ({
            ...node,
            ...normalizedNode,
          }),
        ),
        lastAppliedEventName: 'snapshot.updated',
      }
    })
    logTopologyStoreState('snapshot.updated', get())
  },

  applyPortBound: (event) => {
    set((state) => {
      const currentPorts = state.portsByNode[event.node_name] ?? []
      const nextPorts = upsertPort(currentPorts, event.port)

      return {
        ...state,
        portsByNode: {
          ...state.portsByNode,
          [event.node_name]: nextPorts,
        },
        topologySnapshot: replaceTopologyNode(
          state.topologySnapshot,
          event.node_name,
          (node) => ({
            ...node,
            ports: nextPorts,
          }),
        ),
        lastAppliedEventName: 'port.bound',
      }
    })
    logTopologyStoreState('port.bound', get())
  },

  applyPortReleased: (event) => {
    set((state) => {
      const currentPorts = state.portsByNode[event.node_name] ?? []
      const nextPorts = removePort(currentPorts, event.port)

      return {
        ...state,
        portsByNode: {
          ...state.portsByNode,
          [event.node_name]: nextPorts,
        },
        topologySnapshot: replaceTopologyNode(
          state.topologySnapshot,
          event.node_name,
          (node) => ({
            ...node,
            ports: nextPorts,
          }),
        ),
        lastAppliedEventName: 'port.released',
      }
    })
    logTopologyStoreState('port.released', get())
  },

  applyNodeStatus: (eventName, event) => {
    set((state) => ({
      ...state,
      nodeStatusByNode: {
        ...state.nodeStatusByNode,
        [event.current.node_name]: event.current,
      },
      topologySnapshot: replaceTopologyNode(
        state.topologySnapshot,
        event.current.node_name,
        (node) => ({
          ...node,
          online: event.current.online,
        }),
      ),
      lastAppliedEventName: eventName,
    }))
    logTopologyStoreState(eventName, get())
  },

  applyEdgeDiff: (eventName, event) => {
    set((state) => ({
      ...state,
      topologySnapshot: applyEdgeDiffToSnapshot(state.topologySnapshot, event),
      lastEdgeDiff: event.diff,
      lastAppliedEventName: eventName,
    }))
    logTopologyStoreState(eventName, get())
  },

  reset: () => {
    set({
      topologySnapshot: null,
      portsByNode: {},
      nodeStatusByNode: {},
      lastEdgeDiff: null,
      lastAppliedEventName: null,
    })
  },
}))

export function selectTopologyStoreSummary(
  state: TopologyStoreState,
): TopologyStoreSummary {
  return {
    topologyNodeCount: state.topologySnapshot?.nodes.length ?? 0,
    topologyEdgeCount: state.topologySnapshot?.edges.length ?? 0,
    trackedPortNodeCount: Object.keys(state.portsByNode).length,
    trackedStatusNodeCount: Object.keys(state.nodeStatusByNode).length,
    lastAppliedEventName: state.lastAppliedEventName,
    lastEdgeDiffCounts: {
      added: state.lastEdgeDiff?.added.length ?? 0,
      removed: state.lastEdgeDiff?.removed.length ?? 0,
      changed: state.lastEdgeDiff?.changed.length ?? 0,
    },
  }
}
