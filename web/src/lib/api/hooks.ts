import { useQuery } from "@tanstack/react-query";
import { api, type Host, type MetricsResponse, type ProcessList, type PortList, type ContainerList, type SwarmServiceList, type ComposeProjectList } from "./client";

// Query keys per design §17.4.
export const queryKeys = {
  fleetSummary: ["fleet", "summary"] as const,
  hosts: (filters?: Record<string, string>) => ["hosts", filters ?? {}] as const,
  host: (name: string) => ["host", name] as const,
  metrics: (name: string, window: string) => ["metrics", name, window] as const,
  processes: (name: string) => ["processes", name] as const,
  ports: (name: string) => ["ports", name] as const,
  containers: (name: string) => ["containers", name] as const,
  swarmServices: (name: string) => ["swarm", name] as const,
  composeProjects: (name: string) => ["compose", name] as const,
};

export function useFleetSummary() {
  return useQuery({
    queryKey: queryKeys.fleetSummary,
    queryFn: () => api.fleetSummary(),
  });
}

export function useHosts(filters?: {
  status?: string;
  role?: string;
  environment?: string;
  tag?: string;
  q?: string;
  sort?: string;
  order?: string;
}) {
  return useQuery({
    queryKey: queryKeys.hosts(filters as Record<string, string> | undefined),
    queryFn: () => api.hosts(filters),
  });
}

export function useHost(name: string) {
  return useQuery({
    queryKey: queryKeys.host(name),
    queryFn: () => api.host(name),
  });
}

export function useHostMetrics(name: string, window: string = "15m") {
  return useQuery({
    queryKey: queryKeys.metrics(name, window),
    queryFn: () => api.hostMetrics(name, window),
  });
}

export function useHostProcesses(name: string, sort: string = "cpu") {
  return useQuery({
    queryKey: queryKeys.processes(name),
    queryFn: () => api.hostProcesses(name, sort),
  });
}

export function useHostPorts(name: string) {
  return useQuery({
    queryKey: queryKeys.ports(name),
    queryFn: () => api.hostPorts(name),
  });
}

export function useHostContainers(name: string) {
  return useQuery({
    queryKey: queryKeys.containers(name),
    queryFn: () => api.hostContainers(name),
  });
}

export function useHostSwarmServices(name: string) {
  return useQuery({
    queryKey: queryKeys.swarmServices(name),
    queryFn: () => api.hostSwarmServices(name),
  });
}

export function useHostComposeProjects(name: string) {
  return useQuery({
    queryKey: queryKeys.composeProjects(name),
    queryFn: () => api.hostComposeProjects(name),
  });
}

export type { Host, MetricsResponse, ProcessList, PortList, ContainerList, SwarmServiceList, ComposeProjectList };
