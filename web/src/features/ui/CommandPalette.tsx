import { useState, useEffect, useRef, useMemo, useCallback } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useHosts } from "@/lib/api/hooks";
import { StatusBadge } from "@/features/fleet/StatusBadge";

/*
 * Command palette (⌘K / Ctrl+K). Fuzzy-jump to any host by name. The topbar
 * button opens it via a custom event; the global keydown listener also opens
 * it. Arrow keys move the selection, Enter navigates, Esc closes.
 */

const OPEN_EVENT = "command-palette:open";

export function openCommandPalette(): void {
  window.dispatchEvent(new CustomEvent(OPEN_EVENT));
}

export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [sel, setSel] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const navigate = useNavigate();
  const { data: hosts } = useHosts();

  const openIt = useCallback(() => {
    setOpen(true);
    setQuery("");
    setSel(0);
  }, []);

  // Global ⌘K / Ctrl+K + the topbar custom event.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((o) => !o);
        setQuery("");
        setSel(0);
      }
    };
    const onOpen = () => openIt();
    window.addEventListener("keydown", onKey);
    window.addEventListener(OPEN_EVENT, onOpen);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener(OPEN_EVENT, onOpen);
    };
  }, [openIt]);

  // Focus input + reset on open.
  useEffect(() => {
    if (open) {
      const id = requestAnimationFrame(() => inputRef.current?.focus());
      return () => cancelAnimationFrame(id);
    }
  }, [open]);

  const results = useMemo(() => {
    const all = hosts ?? [];
    if (!query.trim()) return all.slice(0, 8);
    const q = query.toLowerCase();
    const ranked = all
      .map((h) => {
        const name = h.name.toLowerCase();
        const idx = name.indexOf(q);
        const score = idx === 0 ? 2 : idx > 0 ? 1 : name.includes(q) ? 0.5 : -1;
        return { h, score };
      })
      .filter((r) => r.score >= 0)
      .sort((a, b) => b.score - a.score || a.h.name.localeCompare(b.h.name));
    return ranked.slice(0, 8).map((r) => r.h);
  }, [hosts, query]);

  // Clamp selection when results change.
  useEffect(() => {
    setSel((s) => Math.min(s, Math.max(0, results.length - 1)));
  }, [results.length]);

  // Scroll selected item into view.
  useEffect(() => {
    const el = listRef.current?.querySelector(`[data-idx="${sel}"]`);
    el?.scrollIntoView({ block: "nearest" });
  }, [sel]);

  const go = useCallback(
    (name: string) => {
      setOpen(false);
      navigate({ to: "/hosts/$host/metrics", params: { host: name } });
    },
    [navigate],
  );

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-[90] flex items-start justify-center pt-[12vh] px-4"
      onClick={() => setOpen(false)}
    >
      <div className="absolute inset-0 bg-black/40" />
      <div
        className="card-overlay relative w-full max-w-lg overflow-hidden p-0"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Input */}
        <div className="flex items-center gap-3 border-b border-line-subtle px-4 py-3">
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none" className="text-ink-muted">
            <circle cx="7" cy="7" r="5" stroke="currentColor" strokeWidth="1.5" />
            <path d="M11 11L14 14" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setSel(0);
            }}
            onKeyDown={(e) => {
              if (e.key === "ArrowDown") {
                e.preventDefault();
                setSel((s) => Math.min(s + 1, results.length - 1));
              } else if (e.key === "ArrowUp") {
                e.preventDefault();
                setSel((s) => Math.max(s - 1, 0));
              } else if (e.key === "Enter") {
                e.preventDefault();
                const r = results[sel];
                if (r) go(r.name);
              } else if (e.key === "Escape") {
                setOpen(false);
              }
            }}
            placeholder="Jump to host…"
            className="w-full bg-transparent text-sm text-ink-primary outline-none placeholder:text-ink-faint"
          />
          <kbd className="rounded border border-line px-1.5 py-0.5 text-[10px] text-ink-faint">esc</kbd>
        </div>

        {/* Results */}
        <div ref={listRef} className="max-h-80 overflow-auto p-1.5">
          {results.length === 0 ? (
            <div className="px-3 py-8 text-center text-sm text-ink-muted">
              {hosts && hosts.length === 0 ? "No hosts discovered" : `No matches for “${query}”`}
            </div>
          ) : (
            results.map((h, i) => (
              <button
                key={h.name}
                data-idx={i}
                onMouseMove={() => setSel(i)}
                onClick={() => go(h.name)}
                className={`flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm transition-colors ${
                  i === sel ? "bg-surface-raised" : ""
                }`}
              >
                <StatusBadge status={h.status} />
                <span className="font-medium text-ink-primary">{h.name}</span>
                {h.environment && (
                  <span className="ml-auto text-xs text-ink-muted">{h.environment}</span>
                )}
              </button>
            ))
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center gap-4 border-t border-line-subtle px-4 py-2 text-[11px] text-ink-faint">
          <span className="flex items-center gap-1">
            <kbd className="rounded border border-line px-1">↑</kbd>
            <kbd className="rounded border border-line px-1">↓</kbd> navigate
          </span>
          <span className="flex items-center gap-1">
            <kbd className="rounded border border-line px-1">↵</kbd> open
          </span>
          <span className="ml-auto">{results.length} result{results.length === 1 ? "" : "s"}</span>
        </div>
      </div>
    </div>
  );
}
