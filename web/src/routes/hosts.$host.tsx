import { createFileRoute, Link, Outlet, redirect } from "@tanstack/react-router";
import { HostHeader } from "@/features/host/HostHeader";
import { useHost } from "@/lib/api/hooks";
import { useEventStream } from "@/lib/stream/useEventStream";

export const Route = createFileRoute("/hosts/$host")({
  beforeLoad: ({ params, location }) => {
    // /hosts/:host → redirect to /hosts/:host/metrics
    if (location.pathname === `/hosts/${params.host}`) {
      throw redirect({ to: "/hosts/$host/metrics", params });
    }
  },
  component: HostLayout,
});

function HostLayout() {
  const { host } = Route.useParams();
  // Subscribe to host.updated events so the host detail page receives
  // capability/status changes in real time (fix F5).
  useEventStream(["hosts"]);
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
}

const TABS: TabDef[] = [
  { label: "Metrics", to: "/hosts/$host/metrics", cap: "metrics" },
  { label: "Processes", to: "/hosts/$host/processes", cap: "metrics" },
  { label: "Ports", to: "/hosts/$host/ports", cap: "metrics" },
  { label: "Docker", to: "/hosts/$host/docker", cap: "docker" },
  { label: "Systemd", to: "/hosts/$host/systemd" },
];

function TabBar({ host }: { host: string }) {
  const { data } = useHost(host);
  const caps = data?.capabilities;

  return (
    <div className="flex gap-1 border-b border-white/[0.04]">
      {TABS.map((tab) => {
        const disabled = tab.cap ? caps && !caps[tab.cap] : false;
        const reason = tab.cap ? caps?.reasons?.[tab.cap] : undefined;
        const tooltip = reason ? `${tab.label} is disabled: ${reason}` : undefined;

        if (disabled) {
          return (
            <span
              key={tab.to}
              title={tooltip}
              className="tab-item-disabled"
            >
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
          </Link>
        );
      })}
    </div>
  );
}
