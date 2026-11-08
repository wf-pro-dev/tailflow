import { Link } from "@tanstack/react-router";
import type { Host } from "@/lib/api/client";
import { StatusBadge } from "./StatusBadge";
import { relativeTime } from "@/lib/format";

interface Props {
  hosts: Host[];
  isLoading: boolean;
  error: Error | null;
}

export function HostTable({ hosts, isLoading, error }: Props) {
  if (isLoading) {
    return (
      <div className="space-y-1 p-2">
        {[0, 1, 2, 3, 4].map((i) => (
          <div key={i} className="skeleton h-12 rounded-lg" />
        ))}
      </div>
    );
  }

  if (error) {
    return (
      <div className="py-12 text-center text-red-400">
        Error loading hosts: {error.message}
      </div>
    );
  }

  if (hosts.length === 0) {
    return (
      <div className="py-12 text-center text-zinc-500">
        <svg className="mx-auto mb-3 opacity-30" width="48" height="48" viewBox="0 0 48 48" fill="none">
          <rect x="6" y="10" width="36" height="10" rx="2" stroke="currentColor" strokeWidth="2" />
          <rect x="6" y="28" width="36" height="10" rx="2" stroke="currentColor" strokeWidth="2" />
          <circle cx="12" cy="15" r="1.5" fill="currentColor" />
          <circle cx="12" cy="33" r="1.5" fill="currentColor" />
        </svg>
        <p className="text-sm">No hosts discovered</p>
        <p className="mt-1 text-xs text-zinc-600">
          The fleet is empty or the collector hasn't started.
        </p>
      </div>
    );
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-xs uppercase tracking-wider text-zinc-500">
            <th className="px-4 py-3 font-medium">Status</th>
            <th className="px-4 py-3 font-medium">Name</th>
            <th className="px-4 py-3 font-medium">Role</th>
            <th className="px-4 py-3 font-medium">Environment</th>
            <th className="px-4 py-3 font-medium">Tags</th>
            <th className="px-4 py-3 font-medium">Last sample</th>
          </tr>
        </thead>
        <tbody>
          {hosts.map((h) => (
            <tr
              key={h.name}
              className="table-row cursor-pointer"
              onClick={() => window.location.href = `/hosts/${h.name}/metrics`}
            >
              <td className="px-4 py-3">
                <StatusBadge status={h.status} />
              </td>
              <td className="px-4 py-3">
                <Link
                  to="/hosts/$host/metrics"
                  params={{ host: h.name }}
                  className="font-medium text-accent hover:text-accent-hover transition-colors"
                >
                  {h.name}
                </Link>
              </td>
              <td className="px-4 py-3 text-zinc-400">{h.role || "—"}</td>
              <td className="px-4 py-3 text-zinc-400">{h.environment || "—"}</td>
              <td className="px-4 py-3">
                <div className="flex flex-wrap gap-1">
                  {(h.tags ?? []).map((t) => (
                    <span
                      key={t}
                      className="rounded bg-white/[0.06] px-1.5 py-0.5 text-xs text-zinc-400"
                    >
                      {t}
                    </span>
                  ))}
                </div>
              </td>
              <td className="px-4 py-3 text-zinc-500">
                {relativeTime(h.last_sample_at)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="border-t border-white/[0.04] px-4 py-2 text-xs text-zinc-500">
        {hosts.length} {hosts.length === 1 ? "host" : "hosts"}
      </div>
    </div>
  );
}
