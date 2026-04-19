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
