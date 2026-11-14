/*
 * useFleetSamples — one EventSource for the whole fleet's per-host metric
 * samples, deriving two views that the Overview needs but the base
 * "hosts"+"fleet" subscription (owned by the root) does not provide:
 *
 *   1. ring     — a short time series of fleet-wide MEAN cpu/mem and SUMMED
 *                 rx/tx, for the KPI sparklines.
 *   2. perHost  — host -> latest {cpu,mem,rx,tx,t}, for the distribution grid.
 *
 * Samples sharing a timestamp are aggregated into one bucket so a partial
 * fleet within a tick doesn't skew the mean; the bucket flushes when the next
 * timestamp arrives. perHost is throttled to a 2s state flush so a large fleet
 * doesn't re-render the grid on every single sample.
 */

import { useEffect, useRef, useState } from "react";

export interface FleetSample {
  t: number;
  cpu: number; // mean cpu % across hosts in this bucket
  mem: number; // mean mem % across hosts in this bucket
  rx: number; // summed rx bytes/s
  tx: number; // summed tx bytes/s
  count: number; // hosts aggregated into this bucket
}

export interface HostLatest {
  cpu: number;
  mem: number;
  rx: number;
  tx: number;
  t: number;
}

interface Bucket {
  t: number;
  cpu: number;
  mem: number;
  rx: number;
  tx: number;
  count: number;
}

const MAX_POINTS = 90;

export function useFleetSamples(hostNames: string[]) {
  const [ring, setRing] = useState<FleetSample[]>([]);
  const [perHost, setPerHost] = useState<Record<string, HostLatest>>({});
  const bucketRef = useRef<Bucket | null>(null);
  const perHostRef = useRef<Record<string, HostLatest>>({});

  const key = hostNames.join(",");

  useEffect(() => {
    if (hostNames.length === 0) return;
    const topics = hostNames.map((h) => `metrics:${h}`).join(",");
    const url = `/api/v1/stream?topics=${encodeURIComponent(topics)}`;
    const es = new EventSource(url);

    const flushBucket = (b: Bucket) => {
      const sample: FleetSample = {
        t: b.t,
        cpu: b.cpu / b.count,
        mem: b.mem / b.count,
        rx: b.rx,
        tx: b.tx,
        count: b.count,
      };
      setRing((prev) => {
        const last = prev[prev.length - 1];
        if (last && last.t === b.t) return [...prev.slice(0, -1), sample];
        const next = [...prev, sample];
        if (next.length > MAX_POINTS) next.shift();
        return next;
      });
    };

    es.addEventListener("metrics.sample", (e: MessageEvent) => {
      try {
        const d = JSON.parse(e.data);
        if (!d?.t || !d.host) return;
        const cpu = d.cpu_total ?? 0;
        const mem = d.mem_used_pct ?? 0;
        const rx = d.net_rx_bps ?? 0;
        const tx = d.net_tx_bps ?? 0;

        perHostRef.current[d.host] = { cpu, mem, rx, tx, t: d.t };

        const b = bucketRef.current;
        if (!b || b.t !== d.t) {
          if (b) flushBucket(b);
          bucketRef.current = { t: d.t, cpu, mem, rx, tx, count: 1 };
        } else {
          b.cpu += cpu;
          b.mem += mem;
          b.rx += rx;
          b.tx += tx;
          b.count++;
        }
      } catch {
        // ignore malformed sample
      }
    });

    // Throttle per-host state updates so a large fleet doesn't re-render the
    // distribution grid on every single sample.
    const timer = setInterval(() => {
      setPerHost({ ...perHostRef.current });
    }, 2000);

    return () => {
      es.close();
      clearInterval(timer);
      bucketRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);

  return { ring, perHost };
}
