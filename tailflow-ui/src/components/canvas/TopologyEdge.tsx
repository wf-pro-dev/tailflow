import {
  BaseEdge,
  EdgeLabelRenderer,
  getBezierPath,
  type EdgeProps,
} from '@xyflow/react'
import type { TopologyCanvasEdgeData } from './layout'
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
      {data.label ? (
        <EdgeLabelRenderer>
          <div
            className="pointer-events-none absolute -translate-x-1/2 -translate-y-1/2"
            style={{
              left: `${edgeGeometry.labelX}px`,
              top: `${edgeGeometry.labelY}px`,
            }}
          >
            <span
              className={cn(
                'rounded-full border bg-white px-2 py-1 text-[10px] font-medium uppercase tracking-[0.14em]',
                isCyclic
                  ? 'border-amber-200 text-amber-700'
                  : 'border-zinc-200 text-zinc-500',
              )}
            >
              {data.label}
            </span>
          </div>
        </EdgeLabelRenderer>
      ) : null}
    </>
  )
}
