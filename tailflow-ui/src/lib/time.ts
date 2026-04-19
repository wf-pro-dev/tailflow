const relativeTimeFormatter = new Intl.RelativeTimeFormat('en', {
  numeric: 'auto',
})

function toDate(timestamp: string | null | undefined): Date | null {
  if (!timestamp) {
    return null
  }

  const value = new Date(timestamp)
  return Number.isNaN(value.getTime()) ? null : value
}

export function formatTimestamp(timestamp: string | null | undefined): string {
  const value = toDate(timestamp)

  if (!value) {
    return 'Unknown'
  }

  return new Intl.DateTimeFormat('en', {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(value)
}

export function formatRelativeTime(
  timestamp: string | null | undefined,
  now: Date = new Date(),
): string {
  const value = toDate(timestamp)

  if (!value) {
    return 'Unknown'
  }

  const diffSeconds = Math.round((value.getTime() - now.getTime()) / 1000)
  const absSeconds = Math.abs(diffSeconds)

  if (absSeconds < 60) {
    return relativeTimeFormatter.format(diffSeconds, 'second')
  }

  const diffMinutes = Math.round(diffSeconds / 60)
  if (Math.abs(diffMinutes) < 60) {
    return relativeTimeFormatter.format(diffMinutes, 'minute')
  }

  const diffHours = Math.round(diffMinutes / 60)
  if (Math.abs(diffHours) < 24) {
    return relativeTimeFormatter.format(diffHours, 'hour')
  }

  const diffDays = Math.round(diffHours / 24)
  return relativeTimeFormatter.format(diffDays, 'day')
}
