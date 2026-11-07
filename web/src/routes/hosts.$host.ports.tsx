import { createFileRoute } from "@tanstack/react-router";
import { PortTable } from "@/features/host/PortTable";

export const Route = createFileRoute("/hosts/$host/ports")({
  component: PortTableRoute,
});

function PortTableRoute() {
  const { host } = Route.useParams();
  return <PortTable host={host} />;
}
