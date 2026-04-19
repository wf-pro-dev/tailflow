import { cn } from '../../lib/utils'

type StatusTone = 'online' | 'warning' | 'offline' | 'neutral'
type StatusSurface = 'light' | 'dark'

interface StatusDotProps {
  tone: StatusTone
  label: string
  surface?: StatusSurface
  emphasize?: boolean
}

const toneClasses: Record<StatusTone, string> = {
  online: 'bg-green-500',
  warning: 'bg-amber-500',
  offline: 'bg-zinc-500',
  neutral: 'bg-zinc-300',
}

const labelClasses: Record<StatusSurface, string> = {
  light: 'text-zinc-600',
  dark: 'text-zinc-300',
}

export function StatusDot(props: StatusDotProps) {
  return (
    <div className="inline-flex items-center gap-2">
      <span
        aria-hidden="true"
        className={cn('h-2 w-2 rounded-full', toneClasses[props.tone])}
      />
      <span
        className={cn(
          'text-xs font-medium',
          labelClasses[props.surface ?? 'light'],
          props.surface === 'dark' ? 'text-white' : undefined,
        )}
      >
        {props.label}
      </span>
    </div>
  )
}
