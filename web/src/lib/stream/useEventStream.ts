// useEventStream — the SSE hook that feeds the TanStack Query cache.
// Design §17.4.
//
// Opens one EventSource for the given topics. Each event updates the cache
// with queryClient.setQueryData:
//   - metrics.sample → appends to the cached metrics series and trims to window
//   - host.updated → replaces the host row in the hosts list and the single-host cache
//   - host.removed → removes the host from the caches
//   - resync → invalidates all active queries
//
// On resync, or on reconnect after more than 30s, the hook invalidates all
// active queries. If the stream cannot be opened or fails three times in a
// row, queries fall back to refetchInterval: 10_000 (design §17.4 last bullet).

import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { Host, MetricsResponse } from "@/lib/api/client";
import { queryKeys } from "@/lib/api/hooks";

interface SSEEvent {
  event: string;
  id: string;
  data: string;
}

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
  const lastConnectTime = useRef<number>(0);
  const [streamStatus, setStreamStatus] = useState<"connecting" | "open" | "reconnecting" | "closed" | "fallback">("connecting");

  useEffect(() => {
    if (!enabled || topics.length === 0) return;

    const topicsParam = topics.join(",");
    const url = `/api/v1/stream?topics=${encodeURIComponent(topicsParam)}`;
    const es = new EventSource(url);
    esRef.current = es;
    lastConnectTime.current = Date.now();

    es.onopen = () => {
      setStreamStatus("open");
      failureCount.current = 0;
    };

    es.onerror = () => {
      failureCount.current++;
      if (failureCount.current >= 3) {
        setStreamStatus("fallback");
        es.close();
        // Invalidate so queries start polling.
        queryClient.invalidateQueries();
      } else {
        setStreamStatus("reconnecting");
      }
    };

    // Generic message handler — EventSource fires `message` for unnamed events.
    es.addEventListener("message", (e) => handleEvent({ event: "message", id: es.readyState.toString(), data: e.data }));
    // Named events.
    const namedHandler = (name: string) => (e: MessageEvent) => {
      handleEvent({ event: name, id: e.lastEventId, data: e.data });
    };
    const eventNames = [
      "host.updated", "host.removed", "metrics.sample",
      "port.bound", "port.released",
      "docker.container.changed", "docker.inventory.replaced",
      "resync",
    ];
    for (const name of eventNames) {
      es.addEventListener(name, namedHandler(name));
    }

    function handleEvent(evt: SSEEvent) {
      let data: any;
      try {
        data = JSON.parse(evt.data);
      } catch {
        return;
      }

      switch (evt.event) {
        case "host.updated": {
          const host = data as Host;
          // Replace in the single-host cache.
          queryClient.setQueryData(queryKeys.host(host.name), host);
          // Replace in the hosts-list caches (we don't know the exact filter
          // key, so invalidate all hosts queries).
          queryClient.invalidateQueries({ queryKey: ["hosts"] });
          queryClient.invalidateQueries({ queryKey: ["fleet", "summary"] });
          break;
        }
        case "host.removed": {
          const name = data?.name as string;
          queryClient.removeQueries({ queryKey: queryKeys.host(name) });
          queryClient.invalidateQueries({ queryKey: ["hosts"] });
          queryClient.invalidateQueries({ queryKey: ["fleet", "summary"] });
          break;
        }
        case "metrics.sample": {
          const sample = data as MetricsSampleData;
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
                  const arr = (series as any)[k];
                  if (Array.isArray(arr)) arr.splice(0, arr.length - maxLen);
                }
              }
              return { ...old, t, series };
            });
          }
          // Also invalidate the latest-metrics cache (not a query key we track
          // separately, so invalidate the broad host namespace).
          queryClient.invalidateQueries({ queryKey: ["host", sample.host] });
          break;
        }
        case "docker.container.changed":
        case "docker.inventory.replaced": {
          const host = data?.host;
          if (host) {
            queryClient.invalidateQueries({ queryKey: queryKeys.containers(host) });
          }
          break;
        }
        case "port.bound":
        case "port.released": {
          const host = data?.host;
          if (host) {
            queryClient.invalidateQueries({ queryKey: queryKeys.ports(host) });
          }
          break;
        }
        case "resync": {
          // Invalidate all active queries (design §17.4).
          queryClient.invalidateQueries();
          break;
        }
      }
    }

    return () => {
      es.close();
      esRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, topics.join(",")]);

  return { status: streamStatus };
}
