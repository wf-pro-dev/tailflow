import { createFileRoute, Link } from "@tanstack/react-router";
import { useHosts } from "@/lib/api/hooks";
import { useStreamStatus } from "@/lib/stream/statusStore";
import { StatusBadge } from "@/features/fleet/StatusBadge";
import { StreamBanner } from "@/components/StreamBanner";
import { relativeTime } from "@/lib/format";
import type { Host } from "@/lib/api/client";

export const Route = createFileRoute("/hosts/")({
  component: HostsPage,
});

function HostsPage() {
  const { data: hosts, isLoading, error } = useHosts();
  const status = useStreamStatus();

  return (
    <div className="space-y-6">
      <StreamBanner status={status} />

      <div className="flex items-center justify-between">
        <h1 className="text-xl font-bold tracking-tight">Hosts</h1>
        <span className="text-sm text-zinc-500">
          {hosts?.length ?? 0} {(hosts?.length ?? 0) === 1 ? "host" : "hosts"}
        </span>
      </div>

      {isLoading && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {[0, 1, 2, 3, 4, 5].map((i) => (
            <div key={i} className="card">
              <div className="skeleton h-4 w-24 rounded" />
              <div className="skeleton mt-3 h-6 w-32 rounded" />
              <div className="skeleton mt-3 h-3 w-20 rounded" />
            </div>
          ))}
        </div>
      )}

      {error && (
        <div className="card text-danger">Error: {error.message}</div>
      )}

      {!isLoading && !error && hosts && hosts.length === 0 && (
        <div className="card py-16 text-center text-zinc-500">
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
      )}

      {!isLoading && !error && hosts && hosts.length > 0 && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {hosts.map((h) => (
            <HostCard key={h.name} host={h} />
          ))}
        </div>
      )}
    </div>
  );
}

function HostCard({ host }: { host: Host }) {
  return (
    <Link
      to="/hosts/$host/metrics"
      params={{ host: host.name }}
      className="card card-hover block"
    >
      {/* Status + name */}
      <div className="flex items-center justify-between">
        <StatusBadge status={host.status} />
        <span className="text-xs text-zinc-600">{relativeTime(host.last_sample_at)}</span>
      </div>

      {/* Hostname */}
      <h3 className="mt-3 text-lg font-semibold tracking-tight text-zinc-100">
        {host.name}
      </h3>

      {/* Metadata */}
      <div className="mt-2 flex flex-wrap gap-3 text-xs text-zinc-500">
        {host.role && (
          <span>
            <span className="text-zinc-600">Role:</span>{" "}
            <span className="text-zinc-400">{host.role}</span>
          </span>
        )}
        {host.environment && (
          <span>
            <span className="text-zinc-600">Env:</span>{" "}
            <span className="text-zinc-400">{host.environment}</span>
          </span>
        )}
        {host.os && (
          <span>
            <span className="text-zinc-600">OS:</span>{" "}
            <span className="text-zinc-400">{host.os}</span>
          </span>
        )}
      </div>

      {/* Tags */}
      {host.tags && host.tags.length > 0 && (
        <div className="mt-3 flex flex-wrap gap-1">
          {host.tags.map((t) => (
            <span
              key={t}
              className="rounded bg-surface-raised px-1.5 py-0.5 text-xs text-zinc-400"
            >
              {t}
            </span>
          ))}
        </div>
      )}

      {/* Capabilities */}
      {host.capabilities && (
        <div className="mt-3 flex gap-3 border-t border-line-subtle pt-3 text-xs">
          <CapIndicator label="Metrics" enabled={host.capabilities.metrics} />
          <CapIndicator label="Docker" enabled={host.capabilities.docker} />
          {host.capabilities.docker && (
            <>
              <CapIndicator label="Swarm" enabled={host.capabilities.swarm} />
              <CapIndicator label="Compose" enabled={host.capabilities.compose} />
            </>
          )}
        </div>
      )}

      {/* IP */}
      {host.ts_ips && host.ts_ips.length > 0 && (
        <div className="mt-3 font-mono text-xs text-zinc-600">
          {host.ts_ips[0]}
        </div>
      )}
    </Link>
  );
}

function CapIndicator({ label, enabled }: { label: string; enabled: boolean }) {
  return (
    <span className={`flex items-center gap-1 ${enabled ? "text-zinc-400" : "text-zinc-600"}`}>
      <span className={`h-1.5 w-1.5 rounded-full ${enabled ? "bg-status-healthy" : "bg-zinc-600"}`} />
      {label}
    </span>
  );
}
