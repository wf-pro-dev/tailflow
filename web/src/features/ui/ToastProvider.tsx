import { createContext, useContext, useState, useCallback, useEffect, useRef, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { Host, Status } from "@/lib/api/client";

/*
 * Toast system + status-transition watcher.
 *
 * The watcher subscribes to the TanStack Query cache and detects when a host's
 * status changes between samples (e.g. healthy → degraded). This is cache-
 * driven (not stream-driven) so it works regardless of which EventSource
 * patched the data. On a transition it pushes a toast.
 */

export type ToastTone = "info" | "success" | "warn" | "error";

export interface Toast {
  id: string;
  title: string;
  desc?: string;
  tone: ToastTone;
}

interface ToastCtx {
  toasts: Toast[];
  push: (t: Omit<Toast, "id">) => void;
  dismiss: (id: string) => void;
}

const Ctx = createContext<ToastCtx | null>(null);

const STATUS_LABEL: Record<Status, string> = {
  healthy: "Healthy",
  degraded: "Degraded",
  stale: "Stale",
  "no-agent": "No agent",
  offline: "Offline",
};

function toneForStatus(s: Status): ToastTone {
  if (s === "healthy") return "success";
  if (s === "degraded" || s === "stale") return "warn";
  return "error";
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const qc = useQueryClient();
  const prevStatuses = useRef<Map<string, Status>>(new Map());

  const dismiss = useCallback((id: string) => {
    setToasts((t) => t.filter((x) => x.id !== id));
  }, []);

  const push = useCallback((t: Omit<Toast, "id">) => {
    const id = `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
    setToasts((prev) => [...prev.slice(-3), { ...t, id }]);
    setTimeout(() => dismiss(id), 5000);
  }, [dismiss]);

  // Status-transition watcher.
  useEffect(() => {
    const gather = (): Map<string, Status> => {
      const m = new Map<string, Status>();
      for (const q of qc.getQueryCache().getAll()) {
        const key = q.queryKey;
        if (!Array.isArray(key)) continue;
        if (key[0] === "host" && typeof key[1] === "string") {
          const h = q.state.data as Host | undefined;
          if (h?.name && h.status) m.set(h.name, h.status);
        } else if (key[0] === "hosts") {
          const arr = q.state.data as Host[] | undefined;
          if (Array.isArray(arr)) for (const h of arr) if (h?.name && h.status) m.set(h.name, h.status);
        }
      }
      return m;
    };

    const unsub = qc.getQueryCache().subscribe((event) => {
      const key = (event.query.queryKey as unknown);
      if (!Array.isArray(key)) return;
      if (key[0] !== "host" && key[0] !== "hosts") return;
      const next = gather();
      const prev = prevStatuses.current;
      for (const [name, status] of next) {
        const old = prev.get(name);
        if (old && old !== status) {
          push({
            title: `${name} → ${STATUS_LABEL[status]}`,
            desc: `was ${STATUS_LABEL[old]}`,
            tone: toneForStatus(status),
          });
        }
      }
      prevStatuses.current = next;
    });

    return unsub;
  }, [qc, push]);

  return (
    <Ctx.Provider value={{ toasts, push, dismiss }}>
      {children}
      <ToastViewport toasts={toasts} dismiss={dismiss} />
    </Ctx.Provider>
  );
}

function ToastViewport({ toasts, dismiss }: { toasts: Toast[]; dismiss: (id: string) => void }) {
  if (toasts.length === 0) return null;
  return (
    <div className="pointer-events-none fixed bottom-4 right-4 z-[100] flex w-80 flex-col gap-2">
      {toasts.map((t) => (
        <div
          key={t.id}
          role="status"
          className="pointer-events-auto flex items-start gap-3 rounded-lg border border-line bg-surface-overlay px-3 py-2.5 shadow-overlay"
          style={{ animation: "slide-in-right 0.25s ease-out" }}
        >
          <span className={`mt-1 h-2 w-2 shrink-0 rounded-full ${toneDot(t.tone)}`} />
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-ink-primary">{t.title}</div>
            {t.desc && <div className="text-xs text-ink-muted">{t.desc}</div>}
          </div>
          <button
            onClick={() => dismiss(t.id)}
            className="text-ink-faint hover:text-ink-secondary"
            aria-label="Dismiss"
          >
            <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
              <path d="M3 3L11 11M11 3L3 11" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
            </svg>
          </button>
        </div>
      ))}
    </div>
  );
}

function toneDot(tone: ToastTone): string {
  switch (tone) {
    case "success": return "bg-status-healthy";
    case "warn": return "bg-status-degraded";
    case "error": return "bg-danger";
    default: return "bg-accent";
  }
}

export function useToast(): ToastCtx {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useToast must be used within ToastProvider");
  return ctx;
}
