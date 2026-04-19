import { fetchJson } from './client'
import type { HealthResponse } from './types'

export function fetchHealth(): Promise<HealthResponse> {
  return fetchJson<HealthResponse>('/api/v1/health')
}
