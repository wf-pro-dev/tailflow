import { createFileRoute, Link, Outlet, redirect } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { HostHeader } from "@/features/host/HostHeader";
import { useHost, queryKeys } from "@/lib/api/hooks";
import { api } from "@/lib/api/client";
import { useEventStream } from "@/lib/stream/useEventStream";

export const Route = createFileRoute("/hosts/$host")({
  beforeLoad: ({ params, location }) => {
    if (location.pathname === `/hosts/${params.host}`) {
      throw redirect({ to: "/hosts/$host/metrics", params });
    }
  },
  component: HostLayout,
});

function HostLayout() {
  const { host } = Route.useParams();
  // Root owns the base "hosts"+"fleet" subscription; here we only add the
  // host-specific topics (metrics + docker) so there's no topic overlap and
  // thus no double-processing of shared events.
  useEventStream([`metrics:${host}`, `docker:${host}`]);
  return (
    <div className="space-y-4">
      <HostHeader hostName={host} />
      <TabBar host={host} />
      <Outlet />
    </div>
  );
}

interface TabDef {
  label: string;
  to: string;
  cap?: "metrics" | "docker" | "swarm" | "compose";
  /** Which count to show next to the label, if any. */
  count?: "processes" | "ports" | "containers" | "systemd";
}

const TABS: TabDef[] = [
  { label: "Metrics", to: "/hosts/$host/metrics", cap: "metrics" },
  { label: "Processes", to: "/hosts/$host/processes", cap: "metrics", count: "processes" },
  { label: "Ports", to: "/hosts/$host/ports", cap: "metrics", count: "ports" },
  { label: "Docker", to: "/hosts/$host/docker", cap: "docker", count: "containers" },
  { label: "Systemd", to: "/hosts/$host/systemd", count: "systemd" },
];

/** Eager light queries for tab counts. Reuses the same cache keys the tab
 *  pages use, so navigating into a tab is instant. Enabled only when the
 *  relevant capability is present to avoid 404s on incapable hosts. */
function useTabCounts(host: string, caps?: { metrics: boolean; docker: boolean }) {
  const containers = useQuery({
    queryKey: queryKeys.containers(host),
    queryFn: () => api.hostContainers(host),
    enabled: !!caps?.docker,
  });
  const ports = useQuery({
    queryKey: queryKeys.ports(host),
    queryFn: () => api.hostPorts(host),
    enabled: !!caps?.metrics,
  });
  const processes = useQuery({
    queryKey: queryKeys.processes(host),
    queryFn: () => api.hostProcesses(host),
    enabled: !!caps?.metrics,
  });
  const systemd = useQuery({
    queryKey: ["systemd", host],
    queryFn: () => api.systemdUnits(host),
  });
  return {
    containers: containers.data?.containers.length,
    ports: ports.data?.ports.length,
    processes: processes.data?.processes.length,
    systemd: systemd.data?.units.length,
  };
}

function TabBar({ host }: { host: string }) {
  const { data } = useHost(host);
  const caps = data?.capabilities;
  const counts = useTabCounts(host, caps);

  return (
    <div className="flex gap-1 border-b border-line-subtle">
      {TABS.map((tab) => {
        const disabled = tab.cap ? caps && !caps[tab.cap] : false;
        const reason = tab.cap ? caps?.reasons?.[tab.cap] : undefined;
        const tooltip = reason ? `${tab.label} is disabled: ${reason}` : undefined;
        const count = tab.count ? counts[tab.count] : undefined;

        if (disabled) {
          return (
            <span key={tab.to} title={tooltip} className="tab-item-disabled">
              {tab.label}
            </span>
          );
        }

        return (
          <Link
            key={tab.to}
            to={tab.to}
            params={{ host }}
            activeProps={{ className: "tab-item-active" }}
            inactiveProps={{ className: "tab-item" }}
          >
            {tab.label}
            {count != null && (
              <span className="ml-1.5 rounded-full bg-surface-raised px-1.5 text-[10px] tabular-nums text-ink-muted">
                {count}
              </span>
            )}
          </Link>
        );
      })}
    </div>
  );
}
