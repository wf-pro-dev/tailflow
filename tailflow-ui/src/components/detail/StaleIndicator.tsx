import { formatRelativeTime } from '../../lib/time'
import { isStale } from '../../lib/stale'

interface StaleIndicatorProps {
  timestamp: string | null | undefined
}

export function StaleIndicator(props: StaleIndicatorProps) {
  const stale = isStale(props.timestamp)

  return (
    <div
      className={
        stale
          ? 'rounded-xl border border-amber-200 bg-amber-50 px-3 py-2'
          : 'rounded-xl border border-zinc-200 bg-canvas px-3 py-2'
      }
    >
      <p className="text-[11px] uppercase tracking-[0.18em] text-zinc-500">
        Last seen
      </p>
      <p
        className={
          stale
            ? 'mt-1 text-sm font-medium text-amber-700'
            : 'mt-1 text-sm font-medium text-zinc-700'
        }
      >
        {formatRelativeTime(props.timestamp)}
      </p>
    </div>
  )
}
