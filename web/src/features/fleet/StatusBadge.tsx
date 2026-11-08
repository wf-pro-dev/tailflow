import type { Status } from "@/lib/api/client";

const STATUS_LABELS: Record<Status, string> = {
  healthy: "Healthy",
  degraded: "Degraded",
  stale: "Stale",
  "no-agent": "No agent",
  offline: "Offline",
};

const STATUS_BG: Record<Status, string> = {
  healthy: "bg-green-500/10 text-green-400",
  degraded: "bg-amber-500/10 text-amber-400",
  stale: "bg-lime-500/10 text-lime-400",
  "no-agent": "bg-purple-500/10 text-purple-400",
  offline: "bg-zinc-500/10 text-zinc-400",
};

export function StatusBadge({ status }: { status: Status }) {
  return (
    <span className={`badge ${STATUS_BG[status]}`}>
      <span className={`status-dot ${status === "healthy" ? "status-dot-pulse" : ""} status-${status}`} />
      {STATUS_LABELS[status]}
    </span>
  );
}
