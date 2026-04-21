import { useEffect, useRef, useState } from 'react'
import {
  BaseEdge,
  EdgeLabelRenderer,
  getBezierPath,
  type EdgeProps,
} from '@xyflow/react'
import type { TopologyCanvasEdgeData } from './layout'
import { formatTopologyEdgeEndpointText } from '../../lib/topology'
import { cn } from '../../lib/utils'

function buildSelfLoopPath(
  sourceX: number,
  sourceY: number,
): { path: string; labelX: number; labelY: number } {
  const startX = sourceX
  const startY = sourceY - 6
  const loopWidth = 48
  const loopHeight = 56
  const labelX = startX + loopWidth * 0.15
  const labelY = startY - loopHeight - 8

  return {
    path: [
      `M ${startX} ${startY}`,
      `C ${startX + loopWidth} ${startY - 10}, ${startX + loopWidth} ${startY - loopHeight}, ${startX} ${startY - loopHeight}`,
      `C ${startX - 14} ${startY - loopHeight}, ${startX - 14} ${startY - 16}, ${startX} ${startY + 4}`,
    ].join(' '),
    labelX,
    labelY,
  }
}

function buildParallelPath(
  sourceX: number,
  sourceY: number,
  targetX: number,
  targetY: number,
  offset: number,
): { path: string; labelX: number; labelY: number } {
  const deltaX = targetX - sourceX
  const deltaY = targetY - sourceY
  const distance = Math.hypot(deltaX, deltaY) || 1
  const normalX = -deltaY / distance
  const normalY = deltaX / distance

  const controlOneX = sourceX + deltaX * 0.28 + normalX * offset
  const controlOneY = sourceY + deltaY * 0.28 + normalY * offset
  const controlTwoX = sourceX + deltaX * 0.72 + normalX * offset
  const controlTwoY = sourceY + deltaY * 0.72 + normalY * offset

  return {
    path: [
      `M ${sourceX} ${sourceY}`,
      `C ${controlOneX} ${controlOneY}, ${controlTwoX} ${controlTwoY}, ${targetX} ${targetY}`,
    ].join(' '),
    labelX: sourceX + deltaX * 0.5 + normalX * offset,
    labelY: sourceY + deltaY * 0.5 + normalY * offset,
  }
}

export function TopologyEdge(props: EdgeProps) {
  const [isOpen, setIsOpen] = useState(false)
  const popoverRef = useRef<HTMLDivElement | null>(null)
  const data = (props.data ?? {}) as Partial<TopologyCanvasEdgeData>
  const isCyclic = data.isCyclic ?? false
  const isSelfLoop = data.isSelfLoop ?? false
  const parallelCount = data.parallelCount ?? 1
  const parallelOffset = data.parallelOffset ?? 0

  const edgeGeometry = isSelfLoop
    ? buildSelfLoopPath(props.sourceX, props.sourceY)
    : parallelCount > 1
      ? buildParallelPath(
          props.sourceX,
          props.sourceY,
          props.targetX,
          props.targetY,
          parallelOffset,
        )
    : (() => {
        const [path, labelX, labelY] = getBezierPath({
          sourceX: props.sourceX,
          sourceY: props.sourceY,
          sourcePosition: props.sourcePosition,
          targetX: props.targetX,
          targetY: props.targetY,
          targetPosition: props.targetPosition,
          curvature: isCyclic ? 0.46 : 0.22,
        })

        return {
          path,
          labelX,
          labelY,
        }
      })()

  useEffect(() => {
    if (!isOpen) {
      return
    }

    const handlePointerDown = (event: MouseEvent) => {
      if (!popoverRef.current?.contains(event.target as Node)) {
        setIsOpen(false)
      }
    }

    window.addEventListener('mousedown', handlePointerDown)
    return () => window.removeEventListener('mousedown', handlePointerDown)
  }, [isOpen])

  return (
    <>
      <BaseEdge
        path={edgeGeometry.path}
        markerEnd={props.markerEnd}
        style={{
          stroke: isCyclic ? '#f59e0b' : '#a1a1aa',
          strokeWidth: isCyclic ? 2 : 1.5,
        }}
      />
      {data.sourceEndpoint ? (
        <EdgeLabelRenderer>
          <div
            className="absolute -translate-x-1/2 -translate-y-1/2 pointer-events-none"
            style={{
              left: `${edgeGeometry.labelX}px`,
              top: `${edgeGeometry.labelY}px`,
            }}
          >
            <div ref={popoverRef} className="relative pointer-events-auto">
              <button
                type="button"
                onClick={() => setIsOpen((current) => !current)}
                className={cn(
                  'nodrag nopan rounded-full border bg-white/95 px-3 py-1.5 text-[10px] font-semibold tracking-[0.04em] shadow-[0_1px_3px_rgba(0,0,0,0.08)] backdrop-blur',
                  isCyclic
                    ? 'border-amber-200 text-amber-700'
                    : 'border-zinc-200 text-zinc-700',
                )}
              >
                {data.sourceEndpoint.name}
              </button>
              {isOpen ? (
                <div
                  className={cn(
                    'absolute left-1/2 top-full z-10 mt-2 min-w-[12rem] -translate-x-1/2 rounded-xl border bg-white/98 p-3 shadow-[0_4px_18px_rgba(0,0,0,0.12)] backdrop-blur',
                    isCyclic
                      ? 'border-amber-200'
                      : 'border-zinc-200',
                  )}
                >
                  <p className="text-[10px] font-semibold uppercase tracking-[0.14em] text-zinc-400">
                    Endpoints
                  </p>
                  <div className="mt-2 space-y-1.5">
                    {(data.popoverItems ?? []).map((item, index) => (
                      <div
                        key={`${item.name}:${item.portLabel}:${index}`}
                        className="rounded-lg bg-canvas px-2.5 py-2 text-[11px] font-medium leading-4 text-zinc-700"
                      >
                        {formatTopologyEdgeEndpointText(item)}
                      </div>
                    ))}
                  </div>
                </div>
              ) : null}
            </div>
          </div>
        </EdgeLabelRenderer>
      ) : null}
    </>
  )
}
