import { useMemo } from "react";
import { useTheme } from "@/lib/theme";

/*
 * Recharts renders colors as SVG presentation attributes, which do NOT support
 * CSS var() references. So we resolve the theme tokens to concrete color
 * strings here, re-reading whenever the resolved theme changes. This keeps
 * charts correct in both dark and light modes.
 */

function resolveVar(name: string): string {
  if (typeof document === "undefined") return "";
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

export interface ChartColors {
  accent: string;
  grid: string;
  axis: string;
  tick: string;
  healthy: string;
  degraded: string;
  stale: string;
  offline: string;
  noagent: string;
  danger: string;
}

export const CHART_SERIES = ["#6BAFD6", "#E8A33D", "#3ECF8E", "#A88AE0", "#6B6B73"] as const;

export function useChartColors(): ChartColors {
  const { resolved } = useTheme();
  return useMemo(
    () => ({
      accent: resolveVar("--accent"),
      grid: resolveVar("--line-subtle"),
      axis: resolveVar("--ink-faint"),
      tick: resolveVar("--ink-muted"),
      healthy: resolveVar("--status-healthy"),
      degraded: resolveVar("--status-degraded"),
      stale: resolveVar("--status-stale"),
      offline: resolveVar("--status-offline"),
      noagent: resolveVar("--status-noagent"),
      danger: resolveVar("--danger"),
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [resolved],
  );
}

/** Map a status string to its chart color. */
export function statusColor(
  colors: ChartColors,
  status: "healthy" | "degraded" | "stale" | "offline" | "no-agent",
): string {
  switch (status) {
    case "healthy": return colors.healthy;
    case "degraded": return colors.degraded;
    case "stale": return colors.stale;
    case "offline": return colors.offline;
    case "no-agent": return colors.noagent;
  }
}
