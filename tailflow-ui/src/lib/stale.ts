import { env } from '../env'

export function isStale(
  timestamp: string | null | undefined,
  thresholdSeconds: number = env.staleThresholdSeconds,
): boolean {
  if (!timestamp) {
    return true
  }

  const value = new Date(timestamp)
  if (Number.isNaN(value.getTime())) {
    return true
  }

  return Date.now() - value.getTime() > thresholdSeconds * 1000
}
