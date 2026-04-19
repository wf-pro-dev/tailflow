import { useRef } from 'react'
import { env } from '../env'

const RENDER_LOOP_WINDOW_MS = 1200
const RENDER_LOOP_THRESHOLD = 35

type TrackedValues = Record<string, unknown>

function isRenderLoopDebugEnabled(): boolean {
  if (typeof window === 'undefined') {
    return env.renderLoopDebug
  }

  const searchParams = new URLSearchParams(window.location.search)
  return env.renderLoopDebug || searchParams.get('debugRenderLoop') === '1'
}

export function useRenderLoopGuard(
  componentName: string,
  trackedValues?: TrackedValues,
) {
  const stateRef = useRef({
    windowStartAt: 0,
    renderCount: 0,
    changedKeyCounts: {} as Record<string, number>,
    previousTrackedValues: null as TrackedValues | null,
  })

  if (!isRenderLoopDebugEnabled()) {
    return
  }

  const now = performance.now()
  const state = stateRef.current

  if (now - state.windowStartAt > RENDER_LOOP_WINDOW_MS) {
    state.windowStartAt = now
    state.renderCount = 0
    state.changedKeyCounts = {}
  }

  state.renderCount += 1

  if (trackedValues) {
    const previousTrackedValues = state.previousTrackedValues

    if (previousTrackedValues) {
      for (const key of Object.keys(trackedValues)) {
        if (!Object.is(trackedValues[key], previousTrackedValues[key])) {
          state.changedKeyCounts[key] = (state.changedKeyCounts[key] ?? 0) + 1
        }
      }
    }

    state.previousTrackedValues = trackedValues
  }

  if (state.renderCount > RENDER_LOOP_THRESHOLD) {
    const changedKeysSummary = Object.entries(state.changedKeyCounts)
      .sort((left, right) => right[1] - left[1])
      .slice(0, 8)
      .map(([key, count]) => `${key}:${count}`)
      .join(', ')

    throw new Error(
      `Render loop detected in ${componentName} (> ${RENDER_LOOP_THRESHOLD} renders in ${RENDER_LOOP_WINDOW_MS}ms)${
        changedKeysSummary ? `; changing keys: ${changedKeysSummary}` : ''
      }`,
    )
  }
}
