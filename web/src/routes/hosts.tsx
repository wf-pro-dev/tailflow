import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/hosts")({
  // /hosts → redirect to the fleet overview (the host table lives there)
  beforeLoad: () => {
    throw redirect({ to: "/" });
  },
});
