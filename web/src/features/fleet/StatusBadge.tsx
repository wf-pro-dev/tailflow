import type { Status } from "@/lib/api/client";

const STATUS_LABELS: Record<Status, string> = {
  healthy: "Healthy",
  degraded: "Degraded",
  stale: "Stale",
  "no-agent": "No agent",
  offline: "Offline",
};

const STATUS_CLASSES: Record<Status, string> = {
  healthy: "bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400",
  degraded: "bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-400",
  stale: "bg-lime-100 text-lime-800 dark:bg-lime-900/30 dark:text-lime-400",
  "no-agent": "bg-purple-100 text-purple-800 dark:bg-purple-900/30 dark:text-purple-400",
  offline: "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400",
};

export function StatusBadge({ status }: { status: Status }) {
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium ${STATUS_CLASSES[status]}`}>
      <span className={`status-dot status-${status}`} />
      {STATUS_LABELS[status]}
    </span>
  );
}
