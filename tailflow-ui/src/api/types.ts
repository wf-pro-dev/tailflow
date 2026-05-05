export type ID = string
export type Timestamp = string
export type NodeName = string

export interface SnapshotSummary {
  collected_at: Timestamp
  port_count: number
  container_count: number
  service_count: number
  forward_count: number
}

export interface NodeResponse {
  name: NodeName
  tailscale_ip: string
  online: boolean
  degraded: boolean
  collector_degraded?: boolean
  workload_degraded?: boolean
  last_seen_at: Timestamp
  collector_error?: string
  workload_issues?: string[]
  snapshot?: SnapshotSummary
}

export interface ListenPort {
  id?: ID
  addr: string
  port: number
  proto: string
  pid: number
  process: string
}

export interface ContainerPublishedPort {
  host_port: number
  target_port: number
  proto: string
  source: string
  mode?: string
}

export interface ContainerSummary {
  id?: ID
  container_id: string
  container_name: string
  image: string
  state: string
  status: string
  service_name?: string
  published_ports: ContainerPublishedPort[]
}

export interface SwarmServicePort {
  id?: ID
  service_id: string
  service_name: string
  host_port: number
  target_port: number
  proto: string
  mode?: string
}

export type EdgeKind =
  | 'proxy_pass'
  | 'container_publish'
  | 'service_publish'
  | 'direct'

export interface TopologyEdge {
  id: ID
  run_id: ID
  from_node: NodeName
  from_port: number
  from_process: string
  from_container: string
  to_node: NodeName
  to_port: number
  to_process: string
  to_container: string
  to_service: string
  to_runtime_node?: NodeName
  to_runtime_container?: string
  kind: EdgeKind
  resolved: boolean
  raw_upstream: string
}

export interface TopologyNodeResponse {
  name: NodeName
  tailscale_ip: string
  online: boolean
  degraded?: boolean
  collector_degraded?: boolean
  workload_degraded?: boolean
  last_seen_at: Timestamp
  collector_error?: string
  workload_issues?: string[]
  ports: ListenPort[]
  containers: ContainerSummary[]
  services: SwarmServicePort[]
}

export type TopologyHealth =
  | 'healthy'
  | 'degraded'
  | 'unresolved'
  | 'unknown'

export interface Service {
  id: ID
  name: string
  kind: string
  role?: string
  primary_node?: NodeName
  runtime_ids: ID[]
  exposure_ids: ID[]
  health?: TopologyHealth
  tags?: string[]
  description?: string
}

export interface Runtime {
  id: ID
  service_id: ID
  node_id: NodeName
  runtime_kind: string
  runtime_name: string
  pid?: number
  container_id?: string
  image?: string
  state?: string
  ports: number[]
  network_names?: string[]
  network_aliases?: string[]
  health?: TopologyHealth
  collected_at?: Timestamp
}

export interface Exposure {
  id: ID
  service_id: ID
  runtime_id?: ID
  node_id?: NodeName
  kind: string
  protocol?: string
  hostname?: string
  path_prefix?: string
  port?: number
  url?: string
  is_primary: boolean
  visibility?: string
  source?: string
  gateway_service_id?: ID
  health?: TopologyHealth
  resolution_status?: string
}

export interface Route {
  id: ID
  kind: string
  source_service_id?: ID
  source_exposure_id?: ID
  target_service_id?: ID
  target_runtime_id?: ID
  display_name: string
  resolved: boolean
  health?: TopologyHealth
  hop_ids: ID[]
  hostnames?: string[]
  input?: string
}

export interface RouteHop {
  id: ID
  route_id: ID
  order: number
  kind: string
  from?: string
  to?: string
  resolved: boolean
  health?: TopologyHealth
  evidence_id?: ID
}

export interface Evidence {
  id: ID
  matched_by: string
  confidence: string
  reason?: string
  raw_value?: string
  warnings?: string[]
}

export interface TopologySummary {
  node_count: number
  service_count: number
  runtime_count: number
  exposure_count: number
  route_count: number
  unresolved_route_count: number
}

export interface TopologyResponse {
  version: number
  nodes: TopologyNodeResponse[]
  services: Service[]
  runtimes: Runtime[]
  exposures: Exposure[]
  routes: Route[]
  route_hops: RouteHop[]
  evidence: Evidence[]
  summary: TopologySummary
  updated_at: Timestamp
}

export interface NodeSnapshot {
  id: ID
  run_id: ID
  node_name: NodeName
  tailscale_ip: string
  collected_at: Timestamp
  ports: ListenPort[]
  containers: ContainerSummary[]
  services: SwarmServicePort[]
  forwards: ForwardAction[]
  error?: string
}

export interface NodeStatus {
  node_name: NodeName
  online: boolean
  degraded: boolean
  last_seen_at: Timestamp
  last_error?: string
}

export interface HealthResponse {
  status: string
  node_count: number
  collector_degraded_node_count?: number
  workload_degraded_node_count?: number
  updated_at: Timestamp
  topology_version: number
  tailnet_ip: string
}

export interface Listener {
  addr?: string
  port: number
}

export interface ForwardTarget {
  raw: string
  kind: string
  host?: string
  port?: number
  socket?: string
}

export interface ForwardAction {
  listener: Listener
  target: ForwardTarget
}

export interface ParseResult {
  forwards: ForwardAction[]
  errors?: string[]
}

export interface ProxyConfigInput {
  id: ID
  node_name: NodeName
  kind: string
  config_path: string
  updated_at: Timestamp
}

export interface SetProxyConfigRequest {
  kind: string
  config_path: string
}

export interface SetProxyConfigResponse {
  config: ProxyConfigInput
  preview: ParseResult
}

export interface ParsedProxyConfigResponse {
  config: ProxyConfigInput
  parsed: ParseResult
}

export interface ApiErrorResponse {
  error: string
  hint?: string
}

export interface EdgeDiff {
  added: TopologyEdge[]
  removed: TopologyEdge[]
  changed: TopologyEdge[]
}

export interface EdgeEvent {
  run_id: ID
  edges: TopologyEdge[]
  diff: EdgeDiff
}

export interface TopologyPatch {
  version: number
  updated_at: Timestamp
  changed_nodes: NodeName[]
  nodes_upserted: TopologyNodeResponse[]
  nodes_removed: NodeName[]
  services_upserted: Service[]
  services_removed: ID[]
  runtimes_upserted: Runtime[]
  runtimes_removed: ID[]
  exposures_upserted: Exposure[]
  exposures_removed: ID[]
  routes_upserted: Route[]
  routes_removed: ID[]
  route_hops_upserted: RouteHop[]
  route_hops_removed: ID[]
  evidence_upserted: Evidence[]
  evidence_removed: ID[]
  summary: TopologySummary
}

export interface TopologyReset {
  reason: string
  snapshot: TopologyResponse
}
