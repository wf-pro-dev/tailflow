import type { ReactNode } from 'react'
import { cn } from '../../lib/utils'

interface TooltipProps {
  children: ReactNode
  content: string
  className?: string
}

export function Tooltip(props: TooltipProps) {
  return (
    <span
      title={props.content}
      aria-label={props.content}
      className={cn('inline-flex items-center', props.className)}
    >
      {props.children}
    </span>
  )
}
