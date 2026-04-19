import { fetchJson } from './client'
import type { CollectionRun, TriggerRunResponse } from './types'

export function fetchRuns(): Promise<CollectionRun[]> {
  return fetchJson<CollectionRun[]>('/api/v1/runs')
}

export function triggerRun(): Promise<TriggerRunResponse> {
  return fetchJson<TriggerRunResponse>('/api/v1/runs', {
    method: 'POST',
  })
}
