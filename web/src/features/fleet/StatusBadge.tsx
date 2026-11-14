import type { Status } from "@/lib/api/client";

// Single source of truth per status: label, badge bg/text, and the dot color
// class (matches the .status-* classes in styles.css). "no-agent" maps to the
// "noagent" token — the prior code built `status-no-agent` which had no CSS
// match, leaving the no-agent dot uncolored.
const STATUS: Record<Status, { label: string; badge: string; dot: string }> = {
  healthy: { label: "Healthy", badge: "bg-status-healthy/10 text-status-healthy", dot: "status-healthy" },
  degraded: { label: "Degraded", badge: "bg-status-degraded/10 text-status-degraded", dot: "status-degraded" },
  stale: { label: "Stale", badge: "bg-status-stale/10 text-status-stale", dot: "status-stale" },
  "no-agent": { label: "No agent", badge: "bg-status-noagent/10 text-status-noagent", dot: "status-noagent" },
  offline: { label: "Offline", badge: "bg-status-offline/10 text-status-offline", dot: "status-offline" },
};

export function StatusBadge({ status }: { status: Status }) {
  const s = STATUS[status];
  return (
    <span className={`badge ${s.badge}`}>
      <span className={`status-dot ${s.dot}`} />
      {s.label}
    </span>
  );
}
