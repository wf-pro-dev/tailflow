import { fetchJson } from './client'
import type { TopologyEdge, TopologyResponse } from './types'
import { normalizeTopologyResponse } from '../lib/topology'

export function fetchTopology(): Promise<TopologyResponse> {
  return fetchJson<TopologyResponse>('/api/v1/topology').then(
    normalizeTopologyResponse,
  )
}

export function fetchTopologyEdges(): Promise<TopologyEdge[]> {
  return fetchJson<TopologyEdge[]>('/api/v1/topology/edges')
}

export function fetchUnresolvedTopologyEdges(): Promise<TopologyEdge[]> {
  return fetchJson<TopologyEdge[]>('/api/v1/topology/edges/unresolved')
}
