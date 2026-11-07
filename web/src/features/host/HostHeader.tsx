import { useHost } from "@/lib/api/hooks";
import { StatusBadge } from "@/features/fleet/StatusBadge";
import { relativeTime } from "@/lib/format";

export function HostHeader({ hostName }: { hostName: string }) {
  const { data: host, isLoading, error } = useHost(hostName);

  if (isLoading) return <div className="text-gray-500">Loading host…</div>;
  if (error) return <div className="text-red-600">Error: {error.message}</div>;
  if (!host) return null;

  return (
    <div className="card">
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-xl font-bold">{host.name}</h1>
          <div className="mt-1 flex flex-wrap items-center gap-3 text-sm text-gray-500 dark:text-gray-400">
            <StatusBadge status={host.status} />
            {host.role && <span>Role: {host.role}</span>}
            {host.environment && <span>Env: {host.environment}</span>}
            {host.ts_dns_name && <span>{host.ts_dns_name}</span>}
          </div>
        </div>
        <div className="text-right text-xs text-gray-500 dark:text-gray-400">
          {host.ts_ips?.map((ip) => (
            <div key={ip}>{ip}</div>
          ))}
        </div>
      </div>
      <div className="mt-3 flex flex-wrap gap-4 text-xs text-gray-500 dark:text-gray-400">
        {host.os && <span>OS: {host.os}</span>}
        <span>Last sample: {relativeTime(host.last_sample_at)}</span>
        <span>Stream interval: {host.stream_interval_s}s</span>
        {host.capabilities && (
          <>
            <span>Metrics: {host.capabilities.metrics ? "✓" : "✗"}</span>
            <span>Docker: {host.capabilities.docker ? "✓" : "✗"}</span>
            {host.capabilities.docker && (
              <>
                <span>Swarm: {host.capabilities.swarm ? "✓" : "✗"}</span>
                <span>Compose: {host.capabilities.compose ? "✓" : "✗"}</span>
              </>
            )}
          </>
        )}
      </div>
      {host.capabilities?.reasons && Object.entries(host.capabilities.reasons).map(([cap, reason]) => (
        <div key={cap} className="mt-2 text-xs text-amber-600 dark:text-amber-400">
          {cap}: {reason}
        </div>
      ))}
    </div>
  );
}
