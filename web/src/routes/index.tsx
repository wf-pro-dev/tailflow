import { createFileRoute } from "@tanstack/react-router";
import { FleetOverview } from "@/features/fleet/FleetOverview";

export const Route = createFileRoute("/")({
  component: FleetOverview,
});
