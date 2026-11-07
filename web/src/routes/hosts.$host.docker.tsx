import { createFileRoute } from "@tanstack/react-router";
import { DockerTab } from "@/features/host/DockerTab";

export const Route = createFileRoute("/hosts/$host/docker")({
  component: DockerTabRoute,
});

function DockerTabRoute() {
  const { host } = Route.useParams();
  return <DockerTab host={host} />;
}
