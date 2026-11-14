/*
 * StateChip — the single, unified state label used across the app.
 *
 * Replaces the two ad-hoc chips that existed before (a badge-style chip in the
 * Docker tab and a text-only chip in the Systemd tab), which coloured the same
 * idea inconsistently. One palette maps every known host/container/systemd
 * state to a status colour; two visual variants cover both layouts:
 *   - "badge": tinted pill (tables with horizontal room — Docker containers)
 *   - "text" : coloured text only (dense tables — Systemd units)
 * Unknown states fall back to a neutral muted style so new states never render
 * colourless.
 */

type Variant = "badge" | "text";

// Text colour per state (union of Docker container states + systemd load/active
// states + host statuses, so the same chip works everywhere).
const TEXT: Record<string, string> = {
  // healthy / running
  running: "text-status-healthy",
  active: "text-status-healthy",
  healthy: "text-status-healthy",
  // degraded / transitional
  paused: "text-status-degraded",
  deactivating: "text-status-degraded",
  maintenance: "text-status-degraded",
  degraded: "text-status-degraded",
  stale: "text-status-stale",
  // informational
  restarting: "text-accent",
  activating: "text-accent",
  reloading: "text-accent",
  created: "text-status-noagent",
  loaded: "text-ink-secondary",
  inactive: "text-ink-muted",
  exited: "text-status-offline",
  offline: "text-status-offline",
  "no-agent": "text-status-noagent",
  starting: "text-accent",
  // critical
  failed: "text-danger",
  dead: "text-danger",
  unhealthy: "text-danger",
};

// Background tint for the badge variant (mirrors the text palette).
const BG: Record<string, string> = {
  running: "bg-status-healthy/10",
  active: "bg-status-healthy/10",
  healthy: "bg-status-healthy/10",
  paused: "bg-status-degraded/10",
  deactivating: "bg-status-degraded/10",
  maintenance: "bg-status-degraded/10",
  degraded: "bg-status-degraded/10",
  stale: "bg-status-stale/10",
  restarting: "bg-accent/10",
  activating: "bg-accent/10",
  reloading: "bg-accent/10",
  created: "bg-status-noagent/10",
  exited: "bg-status-offline/10",
  offline: "bg-status-offline/10",
  "no-agent": "bg-status-noagent/10",
  starting: "bg-accent/10",
  failed: "bg-danger/10",
  dead: "bg-danger/10",
  unhealthy: "bg-danger/10",
};

export function StateChip({ state, variant = "badge" }: { state: string; variant?: Variant }) {
  const text = TEXT[state] ?? "text-ink-muted";
  if (variant === "text") {
    return <span className={`text-xs font-medium ${text}`}>{state}</span>;
  }
  const bg = BG[state] ?? "bg-surface-raised";
  return <span className={`badge ${bg} ${text}`}>{state}</span>;
}
