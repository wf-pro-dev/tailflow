import type {
  ContainerSummary,
  Evidence,
  Exposure,
  ListenPort,
  Route,
  RouteHop,
  Runtime,
  Service,
  SwarmServicePort,
  TopologySummary,
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

function normalizeTopologyServices(
  services: Service[] | null | undefined,
): Service[] {
  return Array.isArray(services) ? services : []
}

function normalizeRuntimes(
  runtimes: Runtime[] | null | undefined,
): Runtime[] {
  return Array.isArray(runtimes) ? runtimes : []
}

function normalizeExposures(
  exposures: Exposure[] | null | undefined,
): Exposure[] {
  return Array.isArray(exposures) ? exposures : []
}

function normalizeRoutes(routes: Route[] | null | undefined): Route[] {
  return Array.isArray(routes) ? routes : []
}

function normalizeRouteHops(
  routeHops: RouteHop[] | null | undefined,
): RouteHop[] {
  return Array.isArray(routeHops) ? routeHops : []
}

function normalizeEvidence(
  evidence: Evidence[] | null | undefined,
): Evidence[] {
  return Array.isArray(evidence) ? evidence : []
}

function normalizeSummary(
  summary: TopologySummary | null | undefined,
): TopologySummary {
  return (
    summary ?? {
      node_count: 0,
      service_count: 0,
      runtime_count: 0,
      exposure_count: 0,
      route_count: 0,
      unresolved_route_count: 0,
    }
  )
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
    services?: Service[] | null
    runtimes?: Runtime[] | null
    exposures?: Exposure[] | null
    routes?: Route[] | null
    route_hops?: RouteHop[] | null
    evidence?: Evidence[] | null
  },
): TopologyResponse {
  return {
    ...topology,
    nodes: topology.nodes.map(normalizeTopologyNode),
    services: normalizeTopologyServices(topology.services),
    runtimes: normalizeRuntimes(topology.runtimes),
    exposures: normalizeExposures(topology.exposures),
    routes: normalizeRoutes(topology.routes),
    route_hops: normalizeRouteHops(topology.route_hops),
    evidence: normalizeEvidence(topology.evidence),
    summary: normalizeSummary(topology.summary),
  }
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

export interface TopologyGraphLinkEndpointLabel {
  name: string
  portLabel: string
}

export interface TopologyGraphLink {
  id: string
  route_id: string
  source_service_id?: string
  source_exposure_id?: string
  target_service_id?: string
  target_runtime_id?: string
  from_node: string
  from_port: number
  from_name: string
  to_node: string
  to_port: number
  to_name: string
  to_service: string
  runtime_name?: string
  hostnames?: string[]
  display_name: string
  input?: string
  resolved: boolean
  health?: string
  evidence_label?: string
  evidence_reason?: string
}

function parseNodePortRef(
  value: string | null | undefined,
): { node: string; port: number } {
  const trimmed = value?.trim() ?? ''
  if (!trimmed) {
    return { node: '', port: 0 }
  }

  const match = /^([^:]+):(\d+)$/.exec(trimmed)
  if (!match) {
    return { node: trimmed, port: 0 }
  }

  return {
    node: match[1] ?? '',
    port: Number(match[2] ?? 0),
  }
}

export function buildTopologyGraphLinks(
  topology: TopologyResponse | null | undefined,
): TopologyGraphLink[] {
  if (!topology) {
    return []
  }

  const serviceByID = new Map(topology.services.map((service) => [service.id, service]))
  const runtimeByID = new Map(topology.runtimes.map((runtime) => [runtime.id, runtime]))
  const exposureByID = new Map(topology.exposures.map((exposure) => [exposure.id, exposure]))
  const evidenceByID = new Map(topology.evidence.map((evidence) => [evidence.id, evidence]))
  const hopsByRouteID = new Map<string, RouteHop[]>()

  for (const hop of topology.route_hops) {
    const currentHops = hopsByRouteID.get(hop.route_id) ?? []
    currentHops.push(hop)
    hopsByRouteID.set(hop.route_id, currentHops)
  }

  const links = topology.routes.map((route) => {
    const sourceExposure = route.source_exposure_id
      ? exposureByID.get(route.source_exposure_id)
      : undefined
    const sourceService = route.source_service_id
      ? serviceByID.get(route.source_service_id)
      : undefined
    const targetService = route.target_service_id
      ? serviceByID.get(route.target_service_id)
      : undefined
    const targetRuntime = route.target_runtime_id
      ? runtimeByID.get(route.target_runtime_id)
      : undefined
    const routeHops = [...(hopsByRouteID.get(route.id) ?? [])].sort(
      (left, right) => left.order - right.order,
    )
    const forwardHop = routeHops.find((hop) => hop.kind === 'proxy_forward')
    const terminalHop =
      [...routeHops].reverse().find((hop) => hop.kind === 'direct_host_port') ??
      forwardHop
    const targetEndpoint = parseNodePortRef(terminalHop?.from ?? forwardHop?.to)
    const evidence = terminalHop?.evidence_id
      ? evidenceByID.get(terminalHop.evidence_id)
      : forwardHop?.evidence_id
        ? evidenceByID.get(forwardHop.evidence_id)
        : undefined

    return {
      id: route.id,
      route_id: route.id,
      source_service_id: route.source_service_id,
      source_exposure_id: route.source_exposure_id,
      target_service_id: route.target_service_id,
      target_runtime_id: route.target_runtime_id,
      from_node: sourceExposure?.node_id ?? sourceService?.primary_node ?? '',
      from_port: sourceExposure?.port ?? 0,
      from_name: pickFirstNonEmpty(
        sourceService?.name,
        sourceExposure?.hostname,
        sourceExposure?.url,
      ) || 'gateway',
      to_node:
        targetRuntime?.node_id ??
        targetEndpoint.node ??
        targetService?.primary_node ??
        '',
      to_port:
        targetEndpoint.port ||
        targetRuntime?.ports[0] ||
        0,
      to_name: pickFirstNonEmpty(
        targetRuntime?.runtime_name,
        targetService?.name,
        terminalHop?.to,
        forwardHop?.to,
        route.display_name,
      ) || 'unresolved target',
      to_service: targetService?.name ?? '',
      runtime_name: targetRuntime?.runtime_name,
      hostnames: route.hostnames ?? [],
      display_name: route.display_name,
      input: route.input,
      resolved: route.resolved,
      health: route.health,
      evidence_label: evidence?.matched_by,
      evidence_reason: evidence?.reason,
    } satisfies TopologyGraphLink
  })

  return links.sort((left, right) => {
    if (left.from_node !== right.from_node) {
      return left.from_node.localeCompare(right.from_node)
    }
    if (left.from_port !== right.from_port) {
      return left.from_port - right.from_port
    }
    if (left.to_node !== right.to_node) {
      return left.to_node.localeCompare(right.to_node)
    }
    if (left.to_port !== right.to_port) {
      return left.to_port - right.to_port
    }
    return left.display_name.localeCompare(right.display_name)
  })
}

export function isLocalTopologyGraphLink(link: TopologyGraphLink): boolean {
  return link.from_node === link.to_node
}

export function filterVisibleTopologyGraphLinks(
  links: TopologyGraphLink[] | null | undefined,
): TopologyGraphLink[] {
  return Array.isArray(links)
    ? links.filter((link) => link.from_node.length > 0 && link.to_node.length > 0)
    : []
}

export function buildTopologyGraphLinkEndpointLabel(
  link: TopologyGraphLink,
  endpoint: 'source' | 'target',
): TopologyGraphLinkEndpointLabel {
  if (endpoint === 'source') {
    return {
      name: link.from_name || link.from_node || 'unknown source',
      portLabel: formatTopologyEdgePortLabel(link.from_port),
    }
  }

  return {
    name: link.to_name || link.to_node || 'unresolved target',
    portLabel: formatTopologyEdgePortLabel(link.to_port),
  }
}

export function formatTopologyGraphLinkEndpointText(
  endpoint: TopologyGraphLinkEndpointLabel,
): string {
  return `${endpoint.name}:${endpoint.portLabel}`
}

export function formatTopologyGraphLinkLabel(link: TopologyGraphLink): string {
  return (
    link.hostnames?.[0] ||
    link.input ||
    link.display_name ||
    `${link.to_node}:${formatTopologyEdgePortLabel(link.to_port)}`
  )
}
