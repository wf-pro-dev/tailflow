// Formatting helpers per design §17.5.
// All formatters return "—" for missing/NaN/null values (never "0" or "NaN").

const IEC_UNITS = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
const SI_RATE_UNITS = ["b/s", "kb/s", "Mb/s", "Gb/s", "Tb/s"];

export function bytes(v: number | null | undefined): string {
  if (v == null || isNaN(v)) return "—";
  let i = 0;
  let n = v;
  while (n >= 1024 && i < IEC_UNITS.length - 1) {
    n /= 1024;
    i++;
  }
  if (i === 0) return `${n} ${IEC_UNITS[i]}`;
  return `${n.toFixed(1)} ${IEC_UNITS[i]}`;
}

export function rate(v: number | null | undefined): string {
  // bits per second for network, decimal prefixes (§17.5).
  if (v == null || isNaN(v)) return "—";
  // Convert bytes/s to bits/s.
  const bps = v * 8;
  let i = 0;
  let n = bps;
  while (n >= 1000 && i < SI_RATE_UNITS.length - 1) {
    n /= 1000;
    i++;
  }
  if (i === 0) return `${n} ${SI_RATE_UNITS[i]}`;
  return `${n.toFixed(1)} ${SI_RATE_UNITS[i]}`;
}

export function percent(v: number | null | undefined, decimals: number = 0): string {
  if (v == null || isNaN(v)) return "—";
  return `${v.toFixed(decimals)}%`;
}

export function duration(seconds: number | null | undefined): string {
  if (seconds == null || isNaN(seconds)) return "—";
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m`;
  return `${Math.floor(seconds)}s`;
}

export function relativeTime(iso: string | null | undefined): string {
  if (!iso) return "—";
  const then = new Date(iso).getTime();
  if (isNaN(then)) return "—";
  const diff = Date.now() - then;
  const sec = Math.floor(diff / 1000);
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const day = Math.floor(hr / 24);
  return `${day}d ago`;
}

export function missing(): string {
  return "—";
}
