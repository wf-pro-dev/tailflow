import { createFileRoute, Link, Outlet, redirect } from "@tanstack/react-router";
import { HostHeader } from "@/features/host/HostHeader";

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

function TabBar({ host }: { host: string }) {
  const tabs = [
    { label: "Metrics", to: "/hosts/$host/metrics" },
    { label: "Processes", to: "/hosts/$host/processes" },
    { label: "Ports", to: "/hosts/$host/ports" },
    { label: "Docker", to: "/hosts/$host/docker" },
  ] as const;

  return (
    <div className="flex gap-1 border-b border-gray-200 dark:border-gray-800">
      {tabs.map((tab) => (
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
      ))}
    </div>
  );
}
