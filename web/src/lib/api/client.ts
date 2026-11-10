// Hand-written API client. Types are generated from openapi.yaml via
// `npm run gen:types` (openapi-typescript). In M2 we use the generated
// types for the response shapes; the fetch wrapper is hand-written.

const BASE = "/api/v1";

export class APIError extends Error {
  constructor(
    public status: number,
    public type: string,
    public title: string,
    public detail: string,
    public host?: string,
  ) {
    super(detail || title);
    this.name = "APIError";
  }
}

async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(`${BASE}${path}`, {
    ...init,
    headers: { Accept: "application/json", ...init?.headers },
  });
  const ct = resp.headers.get("Content-Type") || "";
  if (ct.includes("application/problem+json")) {
    const body = await resp.json();
    throw new APIError(resp.status, body.type, body.title, body.detail, body.host);
  }
  if (!resp.ok) {
    throw new APIError(resp.status, "http-error", `HTTP ${resp.status}`, await resp.text());
  }
  return resp.json() as Promise<T>;
}

// --- Endpoint functions ---

export const api = {
  healthz: () => fetchJSON<{ status: string }>("/healthz"),
  meta: () =>
    fetchJSON<{
      version: string;
      tailkit_version: string;
      uptime_seconds: number;
      buffer_config: {
        history_window: string;
        history_step: string;
        stream_interval: string;
        stale_factor: number;
      };
    }>("/meta"),
  fleetSummary: () =>
    fetchJSON<FleetSummary>("/fleet/summary"),
  hostContainerStats: (host: string) =>
    fetchJSON<{ host: string; stats: Record<string, ContainerStats> }>(
      `/hosts/${encodeURIComponent(host)}/docker/containers/stats`
    ),
  hosts: (params?: {
    status?: string;
    role?: string;
    environment?: string;
    tag?: string;
    q?: string;
    sort?: string;
    order?: string;
  }) => {
    const q = new URLSearchParams();
    if (params) {
      for (const [k, v] of Object.entries(params)) {
        if (v) q.set(k, v);
      }
    }
    const qs = q.toString();
    return fetchJSON<Host[]>(`/hosts${qs ? "?" + qs : ""}`);
  },
  host: (host: string) => fetchJSON<Host>(`/hosts/${encodeURIComponent(host)}`),
  hostMetrics: (host: string, window: string = "15m") =>
    fetchJSON<MetricsResponse>(`/hosts/${encodeURIComponent(host)}/metrics?window=${window}`),
  hostMetricsLatest: (host: string) =>
    fetchJSON<LatestMetrics>(`/hosts/${encodeURIComponent(host)}/metrics/latest`),
  hostProcesses: (host: string, sort: string = "cpu") =>
    fetchJSON<ProcessList>(`/hosts/${encodeURIComponent(host)}/processes?sort=${sort}`),
  hostPorts: (host: string) =>
    fetchJSON<PortList>(`/hosts/${encodeURIComponent(host)}/ports`),
  hostContainers: (host: string) =>
    fetchJSON<ContainerList>(`/hosts/${encodeURIComponent(host)}/docker/containers`),
  hostSwarmServices: (host: string) =>
    fetchJSON<SwarmServiceList>(`/hosts/${encodeURIComponent(host)}/docker/swarm/services`),
  hostComposeProjects: (host: string) =>
    fetchJSON<ComposeProjectList>(`/hosts/${encodeURIComponent(host)}/docker/compose/projects`),
  hostDockerImages: (host: string) =>
    fetchJSON<{ host: string; images: DockerImage[] }>(`/hosts/${encodeURIComponent(host)}/docker/images`),
  systemdUnits: (host: string) =>
    fetchJSON<{ host: string; units: any[] }>(`/hosts/${encodeURIComponent(host)}/systemd/units`),
};

// --- Domain types (mirror the OpenAPI schemas) ---

export type Status = "offline" | "no-agent" | "stale" | "degraded" | "healthy";

export interface Host {
  name: string;
  role?: string;
  environment?: string;
  tags?: string[];
  ts_ips?: string[];
  ts_dns_name?: string;
  os?: string;
  online: boolean;
  status: Status;
  capabilities: {
    metrics: boolean;
    docker: boolean;
    swarm: boolean;
    compose: boolean;
    reasons?: Record<string, string>;
  };
  last_sample_at?: string | null;
  stream_interval_s: number;
  uptime_seconds?: number;
}

export interface MetricsResponse {
  host: string;
  window_s: number;
  step_s: number;
  t: number[];
  series: {
    cpu_total?: (number | null)[];
    cpu_per_core?: (number | null)[][];
    mem_used_pct?: (number | null)[];
    swap_used_pct?: (number | null)[];
    net_rx_bps?: (number | null)[];
    net_tx_bps?: (number | null)[];
    mem_used_bytes?: (number | null)[];
    mem_total_bytes?: (number | null)[];
  };
}

export interface LatestMetrics {
  host: string;
  t: number;
  cpu_total?: number | null;
  mem_used_pct?: number | null;
  mem_used_bytes?: number | null;
  mem_total_bytes?: number | null;
  net_rx_bps?: number | null;
  net_tx_bps?: number | null;
  disks?: { path: string; total: number; used: number; used_pct: number }[];
  net_interfaces?: { name: string; bytes_recv: number; bytes_sent: number }[];
}

export interface Process {
  pid: number;
  name: string;
  status: string;
  cpu_percent: number;
  memory_rss_bytes: number;
  cmdline?: string;
}

export interface ProcessList {
  host: string;
  sort: string;
  truncated_to: number;
  processes: Process[];
}

export interface Port {
  addr: string;
  port: number;
  proto: string;
  pid: number;
  process?: string;
}

export interface PortList {
  host: string;
  ports: Port[];
}

export interface Container {
  id: string;
  name: string;
  image: string;
  state: string;
  status?: string;
  health?: string;
  ports?: { ip?: string; private: number; public?: number; proto?: string }[];
  created_at?: string;
  command?: string;
}

export interface ContainerList {
  host: string;
  containers: Container[];
}

export interface SwarmService {
  id: string;
  name: string;
  image: string;
  mode?: string;
  replicas?: number | null;
  ports?: { published?: number; target: number; proto?: string }[];
}

export interface SwarmServiceList {
  host: string;
  services: SwarmService[];
}

export interface ComposeProject {
  name: string;
  status?: string;
  config_files?: string;
}

export interface ComposeProjectList {
  host: string;
  projects: ComposeProject[];
}

export interface DockerImage {
  id: string;
  tags?: string[];
  size?: number;
  created_at?: number;
}

export interface ContainerStats {
  cpu_percent: number;
  mem_used_bytes: number;
  mem_limit_bytes: number;
  net_rx_bps: number;
  net_tx_bps: number;
}

export interface HostResource {
  name: string;
  cpu_pct: number;
  mem_pct: number;
  disk_pct?: number;
  fleet_share: number;
}

export interface ContainerResource {
  host: string;
  container_id: string;
  name: string;
  image: string;
  cpu_percent: number;
  mem_used_bytes: number;
}

export interface FleetSummary {
  counts: Record<string, number>;
  mean_cpu_pct: number | null;
  mean_mem_pct: number | null;
  containers_running: number;
  total_mem_bytes: number;
  total_mem_used: number;
  total_disk_bytes: number;
  total_disk_used: number;
  host_count: number;
  healthy_count: number;
  top_cpu_hosts: HostResource[];
  top_mem_hosts: HostResource[];
  top_disk_hosts: HostResource[];
  top_containers: ContainerResource[];
}
