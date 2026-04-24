import { useEffect, useRef, useState } from 'react'
import {
  BaseEdge,
  EdgeLabelRenderer,
  getBezierPath,
  type EdgeProps,
} from '@xyflow/react'
import type { TopologyCanvasEdgeData } from './layout'
import { formatTopologyGraphLinkEndpointText } from '../../lib/topology'
import { cn } from '../../lib/utils'

function buildSelfLoopPath(
  sourceX: number,
  sourceY: number,
  targetX: number,
  targetY: number,
): { path: string; labelX: number; labelY: number } {
  const labelX = (sourceX + targetX) / 2
  const labelY = Math.max(sourceY, targetY) + 36

  return {
    path: `M ${sourceX} ${sourceY} L ${targetX} ${targetY}`,
    labelX,
    labelY,
  }
}

function handleGoTo(hostname: string) {
  window.open(`http://${hostname}`, '_blank', 'noopener,noreferrer')
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
    ? buildSelfLoopPath(props.sourceX, props.sourceY, props.targetX, props.targetY)
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
      {!isSelfLoop ? (
        <BaseEdge
          path={edgeGeometry.path}
          markerEnd={props.markerEnd}
          style={{
            stroke: isCyclic ? '#f59e0b' : '#a1a1aa',
            strokeWidth: isCyclic ? 2 : 1.5,
          }}
        />
      ) : null}
      {data.sourceEndpoint ? (
        <EdgeLabelRenderer>
          <div
            className={cn(
              "absolute -translate-x-1/2 pointer-events-none",
              isSelfLoop ? 'translate-y-[250%]' : '-translate-y-1/2'
            )}
            style={{
              left: `${edgeGeometry.labelX}px`,
              top: `${edgeGeometry.labelY}px`,
            }}
          >
            <div ref={popoverRef} className="relative pointer-events-auto">
              <button
                type="button"
                onClick={() => setIsOpen((current) => !current)}
                className='
                nodrag 
                nopan 
                rounded-full
                border bg-white/95 border-zinc-200 
                text-zinc-700 px-3 py-1.5 text-[10px] 
                font-semibold 
                tracking-[0.04em] shadow-[0_1px_3px_rgba(0,0,0,0.08)] backdrop-blur'
              >
                {`${data.endpointCount ?? 0} endpoint${(data.endpointCount ?? 0) === 1 ? '' : 's'}`}
              </button>
              {isOpen && Array.isArray(data.hostnames) && data.hostnames.length > 0 ? (
                <div
                  className='
                  absolute 
                  left-1/2 
                  top-full 
                  border-zinc-200 
                  z-10 mt-2 min-w-[12rem] 
                  -translate-x-1/2 
                  rounded-xl 
                  border bg-white/98 
                  p-3 shadow-[0_4px_18px_rgba(0,0,0,0.12)] backdrop-blur'
                    
                >


                  <div className="flex flex-col space-y-1.5">
                    {data.hostnames.map((hostname, index) => (
                      <button
                        key={`${hostname}:${index}`}
                        type="button"
                        onClick={() => handleGoTo(hostname)}
                        className="flex flex-1 items-center justify-center rounded-lg border border-zinc-200 bg-white p-2 text-xs font-medium text-zinc-700 shadow-sm transition-colors hover:bg-zinc-50 hover:text-zinc-900 disabled:cursor-not-allowed disabled:opacity-40"
                      >
                        {hostname}
                      </button>
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
