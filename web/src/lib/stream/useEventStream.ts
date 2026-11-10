// useEventStream — the SSE hook that feeds the TanStack Query cache.
// Design §17.4. SSE is the single source of truth; no polling when connected.
//
// Opens one EventSource for the given topics. Each event updates the cache
// with queryClient.setQueryData (NOT invalidateQueries — that would refetch):
//   - host.updated → patches the host in the hosts-list cache + single-host cache
//   - host.removed → removes the host from all caches
//   - metrics.sample → appends to the metrics series + patches last_sample_at
//   - resync → invalidates all active queries (the only case that refetches)
//
// On resync, or on 3 consecutive failures, the hook falls back to polling
// (refetchInterval: 10s) and shows a banner. When SSE reconnects, polling
// stops and SSE takes over again.

import { useEffect, useRef, useState, useCallback } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import type { Host, MetricsResponse } from "@/lib/api/client";
import { queryKeys } from "@/lib/api/hooks";

interface MetricsSampleData {
  host: string;
  t: number;
  cpu_total?: number;
  mem_used_pct?: number;
  net_rx_bps?: number;
  net_tx_bps?: number;
}

export function useEventStream(topics: string[], enabled: boolean = true) {
  const queryClient = useQueryClient();
  const esRef = useRef<EventSource | null>(null);
  const failureCount = useRef(0);
  const [streamStatus, setStreamStatus] = useState<"connecting" | "open" | "reconnecting" | "closed" | "fallback">("connecting");
  const pollingTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // Start polling fallback (10s). Called when SSE fails 3 times.
  const startPolling = useCallback((qc: QueryClient) => {
    if (pollingTimerRef.current) return;
    pollingTimerRef.current = setInterval(() => {
      qc.invalidateQueries();
    }, 10_000);
  }, []);

  // Stop polling fallback. Called when SSE reconnects.
  const stopPolling = useCallback(() => {
    if (pollingTimerRef.current) {
      clearInterval(pollingTimerRef.current);
      pollingTimerRef.current = null;
    }
  }, []);

  useEffect(() => {
    if (!enabled || topics.length === 0) return;

    const topicsParam = topics.join(",");
    const url = `/api/v1/stream?topics=${encodeURIComponent(topicsParam)}`;
    const es = new EventSource(url);
    esRef.current = es;

    es.onopen = () => {
      setStreamStatus("open");
      failureCount.current = 0;
      stopPolling(); // SSE is back — stop polling fallback
    };

    es.onerror = () => {
      failureCount.current++;
      if (failureCount.current >= 3) {
        setStreamStatus("fallback");
        es.close();
        // Invalidate once so queries refetch immediately, then start polling.
        queryClient.invalidateQueries();
        startPolling(queryClient);
      } else {
        setStreamStatus("reconnecting");
      }
    };

    // Event handlers — patch the cache directly, NEVER invalidate (except resync).
    es.addEventListener("host.updated", (e: MessageEvent) => {
      const host = safeParse<Host>(e.data);
      if (!host) return;

      // Patch the single-host cache.
      queryClient.setQueryData(queryKeys.host(host.name), host);

      // Patch the host in the hosts-list cache (find and replace by name).
      queryClient.setQueriesData<Host[]>({ queryKey: ["hosts"] }, (old) => {
        if (!old) return old;
        const idx = old.findIndex((h) => h.name === host.name);
        if (idx < 0) return [...old, host];
        const next = [...old];
        next[idx] = host;
        return next;
      });

      // Only invalidate fleet summary if status changed (not on every field update).
      // We can't easily check the old value here, so invalidate — it's a cheap query.
      queryClient.invalidateQueries({ queryKey: queryKeys.fleetSummary });
    });

    es.addEventListener("host.removed", (e: MessageEvent) => {
      const data = safeParse<{ name: string }>(e.data);
      if (!data) return;
      queryClient.removeQueries({ queryKey: queryKeys.host(data.name) });
      queryClient.setQueriesData<Host[]>({ queryKey: ["hosts"] }, (old) => {
        if (!old) return old;
        return old.filter((h) => h.name !== data.name);
      });
      queryClient.invalidateQueries({ queryKey: queryKeys.fleetSummary });
    });

    es.addEventListener("metrics.sample", (e: MessageEvent) => {
      const sample = safeParse<MetricsSampleData>(e.data);
      if (!sample) return;

      // Append to the metrics series cache for each window.
      for (const window of ["5m", "15m", "1h"]) {
        const key = queryKeys.metrics(sample.host, window);
        queryClient.setQueryData<MetricsResponse>(key, (old) => {
          if (!old) return old;
          const t = [...old.t, sample.t];
          const series = { ...old.series };
          if (series.cpu_total) series.cpu_total = [...series.cpu_total, sample.cpu_total ?? null];
          if (series.mem_used_pct) series.mem_used_pct = [...series.mem_used_pct, sample.mem_used_pct ?? null];
          if (series.net_rx_bps) series.net_rx_bps = [...series.net_rx_bps, sample.net_rx_bps ?? null];
          if (series.net_tx_bps) series.net_tx_bps = [...series.net_tx_bps, sample.net_tx_bps ?? null];
          // Trim to 720 points (1h at 5s step).
          const maxLen = 720;
          if (t.length > maxLen) {
            t.splice(0, t.length - maxLen);
            for (const k of Object.keys(series)) {
              const arr = (series as Record<string, unknown>)[k];
              if (Array.isArray(arr)) (arr as unknown[]).splice(0, arr.length - maxLen);
            }
          }
          return { ...old, t, series };
        });
      }

      // Patch last_sample_at in the single-host cache (NOT invalidate — that
      // would refetch the whole host object every 5s).
      queryClient.setQueryData<Host>(queryKeys.host(sample.host), (old) => {
        if (!old) return old;
        return { ...old, last_sample_at: new Date(sample.t * 1000).toISOString() };
      });
    });

    es.addEventListener("docker.container.changed", (e: MessageEvent) => {
      const data = safeParse<{ host: string; container: any }>(e.data);
      if (data?.host) {
        queryClient.invalidateQueries({ queryKey: queryKeys.containers(data.host) });
      }
    });

    es.addEventListener("docker.inventory.replaced", (e: MessageEvent) => {
      const data = safeParse<{ host: string; containers: any[] }>(e.data);
      if (data?.host) {
        // Patch the container list directly instead of invalidating
        queryClient.setQueryData(queryKeys.containers(data.host), {
          host: data.host,
          containers: data.containers ?? [],
        });
      }
    });

    // Container stats — patch individual container stats in the cache
    es.addEventListener("container.stats", (e: MessageEvent) => {
      const data = safeParse<{
        host: string;
        container_id: string;
        cpu_percent: number;
        mem_used_bytes: number;
        mem_limit_bytes: number;
        net_rx_bps: number;
        net_tx_bps: number;
      }>(e.data);
      if (!data?.host || !data.container_id) return;

      // Patch the container stats cache
      queryClient.setQueryData<{ host: string; stats: Record<string, any> }>(
        ["container-stats", data.host],
        (old) => {
          const stats = old?.stats ?? {};
          stats[data.container_id] = {
            cpu_percent: data.cpu_percent,
            mem_used_bytes: data.mem_used_bytes,
            mem_limit_bytes: data.mem_limit_bytes,
            net_rx_bps: data.net_rx_bps,
            net_tx_bps: data.net_tx_bps,
          };
          return { host: data.host, stats };
        }
      );
    });

    // Processes — patch the process list directly
    es.addEventListener("processes.updated", (e: MessageEvent) => {
      const data = safeParse<{ host: string; processes: any[] }>(e.data);
      if (!data?.host) return;
      queryClient.setQueryData(queryKeys.processes(data.host), {
        host: data.host,
        sort: "cpu",
        truncated_to: data.processes?.length ?? 0,
        processes: data.processes ?? [],
      });
    });

    // Fleet summary — patch the fleet summary cache directly
    es.addEventListener("fleet.summary", (e: MessageEvent) => {
      const data = safeParse<any>(e.data);
      if (!data) return;
      queryClient.setQueryData(queryKeys.fleetSummary, data);
    });

    es.addEventListener("port.bound", (e: MessageEvent) => {
      const data = safeParse<{ host: string }>(e.data);
      if (data?.host) {
        queryClient.invalidateQueries({ queryKey: queryKeys.ports(data.host) });
      }
    });

    es.addEventListener("port.released", (e: MessageEvent) => {
      const data = safeParse<{ host: string }>(e.data);
      if (data?.host) {
        queryClient.invalidateQueries({ queryKey: queryKeys.ports(data.host) });
      }
    });

    es.addEventListener("resync", () => {
      // The only event that triggers a full refetch.
      queryClient.invalidateQueries();
    });

    return () => {
      es.close();
      esRef.current = null;
      stopPolling();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, topics.join(",")]);

  return { status: streamStatus };
}

function safeParse<T>(data: string): T | null {
  try {
    return JSON.parse(data) as T;
  } catch {
    return null;
  }
}
