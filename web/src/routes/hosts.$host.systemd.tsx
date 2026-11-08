import { createFileRoute } from "@tanstack/react-router";
import { SystemdTab } from "@/features/host/SystemdTab";

export const Route = createFileRoute("/hosts/$host/systemd")({
  component: SystemdTabRoute,
});

function SystemdTabRoute() {
  const { host } = Route.useParams();
  return <SystemdTab host={host} />;
}
