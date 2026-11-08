import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api/client";

export function SettingsPage() {
  const { data: meta } = useQuery({
    queryKey: ["meta"],
    queryFn: () => api.meta(),
    staleTime: 60_000,
  });

  return (
    <div className="space-y-6">
      <h1 className="text-xl font-bold tracking-tight">Settings</h1>

      <div className="card">
        <h3 className="text-sm font-medium text-zinc-300">About</h3>
        <div className="mt-4 space-y-2 text-sm">
          <Row label="Version" value={meta?.version ?? "—"} />
          <Row label="tailkit" value={meta?.tailkit_version ?? "—"} />
          <Row label="Uptime" value={meta ? `${Math.floor(meta.uptime_seconds / 60)}m ${meta.uptime_seconds % 60}s` : "—"} />
        </div>
      </div>

      <div className="card">
        <h3 className="text-sm font-medium text-zinc-300">Buffer Configuration</h3>
        <div className="mt-4 space-y-2 text-sm">
          <Row label="History window" value={meta?.buffer_config.history_window ?? "—"} />
          <Row label="History step" value={meta?.buffer_config.history_step ?? "—"} />
          <Row label="Stream interval" value={meta?.buffer_config.stream_interval ?? "—"} />
          <Row label="Stale factor" value={`${meta?.buffer_config.stale_factor ?? "—"}`} />
        </div>
      </div>

      <div className="card">
        <h3 className="text-sm font-medium text-zinc-300">Theme</h3>
        <div className="mt-4 flex gap-2">
          <button className="btn-primary">Dark</button>
          <button className="btn-ghost">Light</button>
          <button className="btn-ghost">System</button>
        </div>
        <p className="mt-3 text-xs text-zinc-500">
          Dark mode is recommended for monitoring dashboards.
        </p>
      </div>
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between">
      <span className="text-zinc-500">{label}</span>
      <span className="font-mono text-zinc-300">{value}</span>
    </div>
  );
}
