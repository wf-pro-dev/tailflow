import type { NodeResponse } from '../api/types'
import { isStale } from './stale'

export type NodeStatusTone = 'online' | 'warning' | 'offline'

export interface NodeStatusView {
  label: string
  tone: NodeStatusTone
}

export function getNodeStatusView(node: Pick<
  NodeResponse,
  'online' | 'degraded' | 'last_seen_at'
>): NodeStatusView {
  if (!node.online) {
    return node.degraded
      ? { label: 'Degraded', tone: 'warning' }
      : { label: 'Offline', tone: 'offline' }
  }

  if (node.degraded) {
    return { label: 'Degraded', tone: 'warning' }
  }

  if (isStale(node.last_seen_at)) {
    return { label: 'Stale', tone: 'warning' }
  }

  return { label: 'Online', tone: 'online' }
}
