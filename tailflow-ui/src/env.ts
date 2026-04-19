function readNumberEnv(value: string | undefined, fallback: number): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

function readBooleanEnv(value: string | undefined, fallback: boolean): boolean {
  if (value === undefined) {
    return fallback
  }

  if (value === '1' || value.toLowerCase() === 'true') {
    return true
  }

  if (value === '0' || value.toLowerCase() === 'false') {
    return false
  }

  return fallback
}

export const env = {
  apiBaseUrl: import.meta.env.VITE_API_BASE_URL ?? '',
  staleThresholdSeconds: readNumberEnv(
    import.meta.env.VITE_STALE_THRESHOLD_SECONDS,
    90,
  ),
  sseReconnectDelayMs: readNumberEnv(
    import.meta.env.VITE_SSE_RECONNECT_DELAY_MS,
    3000,
  ),
  renderLoopDebug: readBooleanEnv(
    import.meta.env.VITE_RENDER_LOOP_DEBUG,
    false,
  ),
} as const
