import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api/client";
import { useTheme } from "@/lib/theme";
import { useDensity } from "@/features/ui/DensityProvider";
import { useStreamStatus } from "@/lib/stream/statusStore";
import { duration } from "@/lib/format";

const STREAM_META: Record<string, { label: string; tone: string; desc: string }> = {
  open: { label: "Connected", tone: "text-status-healthy", desc: "Live events streaming. Queries are updated in real time." },
  connecting: { label: "Connecting", tone: "text-ink-muted", desc: "Opening the event stream…" },
  reconnecting: { label: "Reconnecting", tone: "text-status-degraded", desc: "Stream dropped — retrying." },
  fallback: { label: "Polling", tone: "text-status-degraded", desc: "Stream unavailable. Falling back to 10s polling." },
  closed: { label: "Paused", tone: "text-danger", desc: "Stream closed. Live updates paused." },
};

export function SettingsPage() {
  const { data: meta } = useQuery({
    queryKey: ["meta"],
    queryFn: () => api.meta(),
    staleTime: 60_000,
  });
  const { mode, resolved, setMode } = useTheme();
  const { density, setDensity } = useDensity();
  const streamStatus = useStreamStatus();
  const sm = STREAM_META[streamStatus] ?? STREAM_META.connecting;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-bold tracking-tight">Settings</h1>
        <p className="mt-1 text-sm text-ink-muted">
          Appearance, live-stream status, and server configuration.
        </p>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        {/* Appearance */}
        <div className="card">
          <h3 className="text-sm font-medium text-ink-secondary">Appearance</h3>

          <div className="mt-4">
            <div className="metric-label mb-2">Theme</div>
            <div className="flex gap-2">
              <Seg active={mode === "dark"} onClick={() => setMode("dark")}>Dark</Seg>
              <Seg active={mode === "light"} onClick={() => setMode("light")}>Light</Seg>
              <Seg active={mode === "system"} onClick={() => setMode("system")}>System</Seg>
            </div>
            <p className="mt-2 text-xs text-ink-muted">
              {mode === "system"
                ? `Following OS preference — currently ${resolved}.`
                : `Applied — ${resolved} theme.`}{" "}
              Dark is recommended for monitoring dashboards.
            </p>
          </div>

          <div className="mt-5">
            <div className="metric-label mb-2">Density</div>
            <div className="flex gap-2">
              <Seg active={density === "comfortable"} onClick={() => setDensity("comfortable")}>Comfortable</Seg>
              <Seg active={density === "compact"} onClick={() => setDensity("compact")}>Compact</Seg>
            </div>
            <p className="mt-2 text-xs text-ink-muted">
              Compact shows more rows per screen with smaller sparklines.
            </p>
          </div>
        </div>

        {/* Live stream */}
        <div className="card">
          <h3 className="text-sm font-medium text-ink-secondary">Live Stream</h3>
          <div className="mt-4 flex items-center gap-2">
            <span className={`status-dot ${streamDot(streamStatus)}`} />
            <span className={`text-sm font-medium ${sm.tone}`}>{sm.label}</span>
          </div>
          <p className="mt-2 text-xs text-ink-muted">{sm.desc}</p>
          <div className="mt-4 space-y-2 border-t border-line-subtle pt-3 text-sm">
            <Row label="Topics" value="hosts, fleet (+ host-scoped on detail pages)" />
            <Row label="Transport" value="Server-Sent Events" />
          </div>
        </div>

        {/* About */}
        <div className="card">
          <h3 className="text-sm font-medium text-ink-secondary">About</h3>
          <div className="mt-4 space-y-2 text-sm">
            <Row label="Version" value={meta?.version ?? "—"} />
            <Row label="tailkit" value={meta?.tailkit_version ?? "—"} />
            <Row label="Uptime" value={meta ? duration(meta.uptime_seconds) : "—"} />
          </div>
        </div>

        {/* Buffer configuration */}
        <div className="card">
          <h3 className="text-sm font-medium text-ink-secondary">Buffer Configuration</h3>
          <div className="mt-4 space-y-2 text-sm">
            <Row label="History window" value={meta?.buffer_config.history_window ?? "—"} />
            <Row label="History step" value={meta?.buffer_config.history_step ?? "—"} />
            <Row label="Stream interval" value={meta?.buffer_config.stream_interval ?? "—"} />
            <Row label="Stale factor" value={`${meta?.buffer_config.stale_factor ?? "—"}`} />
          </div>
        </div>
      </div>
    </div>
  );
}

function Seg({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button onClick={onClick} className={active ? "seg seg-active" : "seg"}>
      {children}
    </button>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between gap-4">
      <span className="text-ink-muted">{label}</span>
      <span className="font-mono text-ink-secondary">{value}</span>
    </div>
  );
}

function streamDot(s: string): string {
  if (s === "open") return "status-healthy";
  if (s === "reconnecting" || s === "fallback") return "status-degraded";
  if (s === "closed") return "status-offline";
  return "status-offline";
}
