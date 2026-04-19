import { fetchJson } from './client'
import type { NodeResponse } from './types'

export function fetchNodes(): Promise<NodeResponse[]> {
  return fetchJson<NodeResponse[]>('/api/v1/nodes')
}

export function fetchNode(nodeName: string): Promise<NodeResponse> {
  return fetchJson<NodeResponse>(`/api/v1/nodes/${nodeName}`)
}
