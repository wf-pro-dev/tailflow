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
      <div className="space-y-2">
        {[0, 1, 2, 3, 4].map((i) => (
          <div key={i} className="h-12 animate-pulse rounded bg-gray-100 dark:bg-gray-800" />
        ))}
      </div>
    );
  }

  if (error) {
    return (
      <div className="py-8 text-center text-red-600">
        Error loading hosts: {error.message}
      </div>
    );
  }

  if (hosts.length === 0) {
    return (
      <div className="py-8 text-center text-gray-500 dark:text-gray-400">
        No hosts discovered. The fleet is empty or the collector hasn't started.
      </div>
    );
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-gray-200 text-left text-xs uppercase text-gray-500 dark:border-gray-800 dark:text-gray-400">
            <th className="py-2 pr-4">Status</th>
            <th className="py-2 pr-4">Name</th>
            <th className="py-2 pr-4">Role</th>
            <th className="py-2 pr-4">Environment</th>
            <th className="py-2 pr-4">Tags</th>
            <th className="py-2 pr-4">Last sample</th>
          </tr>
        </thead>
        <tbody>
          {hosts.map((h) => (
            <tr
              key={h.name}
              className="border-b border-gray-100 hover:bg-gray-50 dark:border-gray-900 dark:hover:bg-gray-800/50"
            >
              <td className="py-2 pr-4">
                <StatusBadge status={h.status} />
              </td>
              <td className="py-2 pr-4">
                <Link
                  to="/hosts/$host/metrics"
                  params={{ host: h.name }}
                  className="font-medium text-blue-600 hover:underline dark:text-blue-400"
                >
                  {h.name}
                </Link>
              </td>
              <td className="py-2 pr-4 text-gray-600 dark:text-gray-300">{h.role || "—"}</td>
              <td className="py-2 pr-4 text-gray-600 dark:text-gray-300">{h.environment || "—"}</td>
              <td className="py-2 pr-4">
                <div className="flex flex-wrap gap-1">
                  {(h.tags ?? []).map((t) => (
                    <span key={t} className="rounded bg-gray-100 px-1.5 py-0.5 text-xs text-gray-600 dark:bg-gray-800 dark:text-gray-300">
                      {t}
                    </span>
                  ))}
                </div>
              </td>
              <td className="py-2 pr-4 text-gray-500 dark:text-gray-400">
                {relativeTime(h.last_sample_at)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="mt-2 text-xs text-gray-500 dark:text-gray-400">
        {hosts.length} {hosts.length === 1 ? "host" : "hosts"}
      </div>
    </div>
  );
}
