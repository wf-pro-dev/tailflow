import { createFileRoute, Link, Outlet, redirect } from "@tanstack/react-router";
import { HostHeader } from "@/features/host/HostHeader";
import { useHost } from "@/lib/api/hooks";

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
];

function TabBar({ host }: { host: string }) {
  const { data } = useHost(host);
  const caps = data?.capabilities;

  return (
    <div className="flex gap-1 border-b border-gray-200 dark:border-gray-800">
      {TABS.map((tab) => {
        const disabled = tab.cap ? caps && !caps[tab.cap] : false;
        const reason = tab.cap ? caps?.reasons?.[tab.cap] : undefined;
        const tooltip = reason ? `${tab.label} is disabled: ${reason}` : undefined;

        if (disabled) {
          return (
            <span
              key={tab.to}
              title={tooltip}
              className="cursor-not-allowed px-4 py-2 text-sm font-medium text-gray-300 dark:text-gray-600"
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
            activeProps={{ className: "border-b-2 border-blue-600 text-blue-600" }}
            inactiveProps={{ className: "text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200" }}
            className="px-4 py-2 text-sm font-medium"
          >
            {tab.label}
          </Link>
        );
      })}
    </div>
  );
}
