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
  collector_error?: string
  workload_issues?: string[]
  ports: ListenPort[]
  containers: ContainerSummary[]
  services: SwarmServicePort[]
}

export interface TopologyResponse {
  run_id: ID
  nodes: TopologyNodeResponse[]
  edges: TopologyEdge[]
  updated_at: Timestamp
}

export interface CollectionRun {
  id: ID
  started_at: Timestamp
  finished_at: Timestamp
  node_count: number
  error_count: number
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

export interface TriggerRunResponse {
  accepted: boolean
  started_at: Timestamp
}

export interface HealthResponse {
  status: string
  node_count: number
  collector_degraded_node_count?: number
  workload_degraded_node_count?: number
  last_run_at: Timestamp
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

export interface SnapshotEvent {
  run_id: ID
  snapshot: NodeSnapshot
}

export interface NodeStatusEvent {
  previous: NodeStatus
  current: NodeStatus
}

export interface PortBoundEvent {
  node_name: NodeName
  port: ListenPort
}

export interface PortReleasedEvent {
  node_name: NodeName
  port: ListenPort
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
