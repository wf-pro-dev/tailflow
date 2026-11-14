import { useSyncExternalStore } from "react";
import type { StreamStatus } from "./useEventStream";

/*
 * Shared stream-status store. useEventStream instances write their status here;
 * any component (e.g. SettingsPage) can read it via useStreamStatus without
 * opening its own EventSource. Last-write-wins is fine because every
 * EventSource connects to the same server endpoint and will share a status.
 */

let current: StreamStatus = "connecting";
const listeners = new Set<() => void>();

export function setStreamStatus(s: StreamStatus): void {
  if (s === current) return;
  current = s;
  for (const l of listeners) l();
}

function subscribe(cb: () => void): () => void {
  listeners.add(cb);
  return () => listeners.delete(cb);
}

function getSnapshot(): StreamStatus {
  return current;
}

export function useStreamStatus(): StreamStatus {
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}
