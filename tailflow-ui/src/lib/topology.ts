import type {
  ContainerSummary,
  ListenPort,
  SwarmServicePort,
  TopologyEdge,
  TopologyNodeResponse,
  TopologyResponse,
} from '../api/types'

function normalizePorts(ports: ListenPort[] | null | undefined): ListenPort[] {
  return Array.isArray(ports) ? ports : []
}

function normalizeContainers(
  containers: ContainerSummary[] | null | undefined,
): ContainerSummary[] {
  return Array.isArray(containers) ? containers : []
}

function normalizeServices(
  services: SwarmServicePort[] | null | undefined,
): SwarmServicePort[] {
  return Array.isArray(services) ? services : []
}

export function isSelfTopologyEdge(edge: TopologyEdge): boolean {
  return edge.from_node === edge.to_node
}

export function isVisibleTopologyEdge(edge: TopologyEdge): boolean {
  return edge.kind !== 'service_publish' && !isSelfTopologyEdge(edge)
}

export function filterVisibleTopologyEdges(
  edges: TopologyEdge[] | null | undefined,
): TopologyEdge[] {
  return Array.isArray(edges) ? edges.filter(isVisibleTopologyEdge) : []
}

export function normalizeTopologyNode(
  node: TopologyNodeResponse & {
    ports?: ListenPort[] | null
    containers?: ContainerSummary[] | null
    services?: SwarmServicePort[] | null
  },
): TopologyNodeResponse {
  return {
    ...node,
    ports: normalizePorts(node.ports),
    containers: normalizeContainers(node.containers),
    services: normalizeServices(node.services),
  }
}

export function normalizeTopologyResponse(
  topology: TopologyResponse & {
    nodes: Array<
      TopologyNodeResponse & {
        ports?: ListenPort[] | null
        containers?: ContainerSummary[] | null
        services?: SwarmServicePort[] | null
      }
    >
    edges?: TopologyEdge[] | null
  },
): TopologyResponse {
  return {
    ...topology,
    nodes: topology.nodes.map(normalizeTopologyNode),
    edges: Array.isArray(topology.edges)
      ? topology.edges.filter((edge) => edge.kind !== 'service_publish')
      : [],
  }
}

export interface TopologyEdgeEndpointLabel {
  name: string
  portLabel: string
}

function pickFirstNonEmpty(...values: Array<string | null | undefined>): string {
  for (const value of values) {
    if (typeof value === 'string' && value.trim().length > 0) {
      return value.trim()
    }
  }

  return ''
}

export function formatTopologyEdgePortLabel(port: number): string {
  return Number.isFinite(port) && port > 0 ? String(port) : 'n/a'
}

export function buildTopologyEdgeEndpointLabel(
  edge: TopologyEdge,
  endpoint: 'source' | 'target',
): TopologyEdgeEndpointLabel {
  if (endpoint === 'source') {
    const sourceName = pickFirstNonEmpty(
      edge.from_container,
      edge.from_process,
      edge.from_node,
    )

    return {
      name: sourceName || 'unknown source',
      portLabel: formatTopologyEdgePortLabel(edge.from_port),
    }
  }

  const targetName = pickFirstNonEmpty(
    edge.to_runtime_container,
    edge.to_service,
    edge.to_container,
    edge.to_process,
    edge.to_runtime_node,
    edge.to_node,
    edge.raw_upstream,
  )

  return {
    name: targetName || 'unresolved target',
    portLabel: formatTopologyEdgePortLabel(edge.to_port),
  }
}

export function formatTopologyEdgeEndpointText(
  endpoint: TopologyEdgeEndpointLabel,
): string {
  return `${endpoint.name}:${endpoint.portLabel}`
}

export function formatTopologyEdgeLabel(edge: TopologyEdge): string {
  switch (edge.kind) {
    case 'container_publish':
      return edge.to_container || edge.raw_upstream || 'container publish'
    case 'service_publish':
      return edge.to_service || edge.raw_upstream || 'service publish'
    case 'proxy_pass':
      return edge.raw_upstream || edge.to_service || edge.to_container || `${edge.to_node}:${edge.to_port}`
    case 'direct':
      return edge.to_process || edge.raw_upstream || edge.to_service || edge.to_container || `${edge.to_node}:${edge.to_port}`
    default:
      return edge.raw_upstream || edge.kind
  }
}
