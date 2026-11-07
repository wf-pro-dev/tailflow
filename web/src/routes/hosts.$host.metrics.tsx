import { createFileRoute } from "@tanstack/react-router";
import { MetricCharts } from "@/features/host/MetricCharts";

export const Route = createFileRoute("/hosts/$host/metrics")({
  component: MetricChartsRoute,
});

function MetricChartsRoute() {
  const { host } = Route.useParams();
  return <MetricCharts host={host} />;
}
