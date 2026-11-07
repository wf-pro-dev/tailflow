import { createFileRoute } from "@tanstack/react-router";
import { ProcessTable } from "@/features/host/ProcessTable";

export const Route = createFileRoute("/hosts/$host/processes")({
  component: ProcessTableRoute,
});

function ProcessTableRoute() {
  const { host } = Route.useParams();
  return <ProcessTable host={host} />;
}
