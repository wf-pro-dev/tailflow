import { useHost } from "@/lib/api/hooks";
import { StatusBadge } from "@/features/fleet/StatusBadge";
import { relativeTime, duration } from "@/lib/format";

export function HostHeader({ hostName }: { hostName: string }) {
  const { data: host, isLoading, error } = useHost(hostName);

  if (isLoading) return <div className="card"><div className="skeleton h-16 rounded" /></div>;
  if (error) return <div className="card text-red-400">Error: {error.message}</div>;
  if (!host) return null;

  return (
    <div className="card">
      <div className="flex items-start justify-between">
        <div>
          <div className="flex items-center gap-3">
            <StatusBadge status={host.status} />
            <h1 className="text-xl font-bold tracking-tight">{host.name}</h1>
          </div>
          <div className="mt-2 flex flex-wrap items-center gap-4 text-sm text-zinc-500">
            {host.role && <span>Role: <span className="text-zinc-300">{host.role}</span></span>}
            {host.environment && <span>Env: <span className="text-zinc-300">{host.environment}</span></span>}
            {host.ts_dns_name && <span className="font-mono text-xs">{host.ts_dns_name}</span>}
          </div>
        </div>
        <div className="text-right text-xs text-zinc-500">
          {host.ts_ips?.map((ip) => (
            <div key={ip} className="font-mono">{ip}</div>
          ))}
        </div>
      </div>
      <div className="mt-3 flex flex-wrap gap-4 text-xs text-zinc-500">
        {host.os && <span>OS: <span className="text-zinc-400">{host.os}</span></span>}
        <span>Last sample: <span className="text-zinc-400">{relativeTime(host.last_sample_at)}</span></span>
        <span>Stream interval: <span className="text-zinc-400">{host.stream_interval_s}s</span></span>
        {host.uptime_seconds ? (
          <span>Uptime: <span className="text-zinc-400">{duration(host.uptime_seconds)}</span></span>
        ) : null}
      </div>
      {host.capabilities && (
        <div className="mt-3 flex flex-wrap gap-3 text-xs">
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
      {host.capabilities?.reasons && Object.entries(host.capabilities.reasons).map(([cap, reason]) => (
        <div key={cap} className="mt-2 text-xs text-amber-400/80">
          {cap}: {reason}
        </div>
      ))}
    </div>
  );
}

function CapIndicator({ label, enabled }: { label: string; enabled: boolean }) {
  return (
    <span className={`flex items-center gap-1 ${enabled ? "text-zinc-400" : "text-zinc-600"}`}>
      <span className={`h-1.5 w-1.5 rounded-full ${enabled ? "bg-green-500" : "bg-zinc-600"}`} />
      {label}
    </span>
  );
}
