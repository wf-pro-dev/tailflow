import { create } from 'zustand'
import type {
  TopologyPatch,
  TopologyNodeResponse,
  TopologyResponse,
} from '../api/types'
import {
  normalizeTopologyNode,
  normalizeTopologyResponse,
} from '../lib/topology'

interface TopologyStoreState {
  topologySnapshot: TopologyResponse | null
  lastAppliedEventName: string | null
  topologyVersion: number
  applySnapshot: (snapshot: TopologyResponse) => void
  applyTopologyPatch: (patch: TopologyPatch) => void
  reset: () => void
}

export interface TopologyStoreSummary {
  topologyNodeCount: number
  topologyRouteCount: number
  trackedPortNodeCount: number
  trackedStatusNodeCount: number
  lastAppliedEventName: string | null
  topologyVersion: number
}

function upsertByID<T extends { id: string }>(current: T[], updates: T[]): T[] {
  const byID = new Map(current.map((value) => [value.id, value]))
  for (const update of updates) {
    byID.set(update.id, update)
  }
  return [...byID.values()]
}

function removeByID<T extends { id: string }>(current: T[], removedIDs: string[]): T[] {
  if (removedIDs.length === 0) {
    return current
  }
  const removed = new Set(removedIDs)
  return current.filter((value) => !removed.has(value.id))
}

function upsertNodes(current: TopologyNodeResponse[], updates: TopologyNodeResponse[]): TopologyNodeResponse[] {
  const byName = new Map(current.map((node) => [node.name, node]))
  for (const update of updates) {
    byName.set(update.name, normalizeTopologyNode(update))
  }
  return [...byName.values()].sort((left, right) => left.name.localeCompare(right.name))
}

function removeNodes(current: TopologyNodeResponse[], removedNames: string[]): TopologyNodeResponse[] {
  if (removedNames.length === 0) {
    return current
  }
  const removed = new Set(removedNames)
  return current.filter((node) => !removed.has(node.name))
}

function patchTopologySnapshot(topologySnapshot: TopologyResponse, patch: TopologyPatch): TopologyResponse {
  return normalizeTopologyResponse({
    ...topologySnapshot,
    version: patch.version,
    updated_at: patch.updated_at,
    nodes: removeNodes(
      upsertNodes(topologySnapshot.nodes, patch.nodes_upserted),
      patch.nodes_removed,
    ),
    services: removeByID(
      upsertByID(topologySnapshot.services, patch.services_upserted),
      patch.services_removed,
    ),
    runtimes: removeByID(
      upsertByID(topologySnapshot.runtimes, patch.runtimes_upserted),
      patch.runtimes_removed,
    ),
    exposures: removeByID(
      upsertByID(topologySnapshot.exposures, patch.exposures_upserted),
      patch.exposures_removed,
    ),
    routes: removeByID(
      upsertByID(topologySnapshot.routes, patch.routes_upserted),
      patch.routes_removed,
    ),
    route_hops: removeByID(
      upsertByID(topologySnapshot.route_hops, patch.route_hops_upserted),
      patch.route_hops_removed,
    ),
    evidence: removeByID(
      upsertByID(topologySnapshot.evidence, patch.evidence_upserted),
      patch.evidence_removed,
    ),
    summary: patch.summary,
  })
}

function logTopologyStoreState(actionName: string, state: TopologyStoreState) {
  console.debug(`[tailflow:store] ${actionName}`, selectTopologyStoreSummary(state))
}

export const useTopologyStore = create<TopologyStoreState>((set, get) => ({
  topologySnapshot: null,
  lastAppliedEventName: null,
  topologyVersion: 0,

  applySnapshot: (snapshot) => {
    const normalizedSnapshot = normalizeTopologyResponse(snapshot)
    set((state) => ({
      ...state,
      topologySnapshot: normalizedSnapshot,
      lastAppliedEventName: 'topology.snapshot',
      topologyVersion: normalizedSnapshot.version,
    }))
    logTopologyStoreState('topology.snapshot', get())
  },

  applyTopologyPatch: (patch) => {
    set((state) => {
      if (!state.topologySnapshot) {
        return {
          ...state,
          lastAppliedEventName: 'topology.patch',
        }
      }
      if (state.topologyVersion > 0 && patch.version !== state.topologyVersion + 1) {
        return {
          ...state,
          lastAppliedEventName: 'topology.patch.out_of_order',
        }
      }
      const nextTopology = patchTopologySnapshot(state.topologySnapshot, patch)
      return {
        ...state,
        topologySnapshot: nextTopology,
        lastAppliedEventName: 'topology.patch',
        topologyVersion: patch.version,
      }
    })
    logTopologyStoreState('topology.patch', get())
  },

  reset: () => {
    set({
      topologySnapshot: null,
      lastAppliedEventName: null,
      topologyVersion: 0,
    })
  },
}))

export function selectTopologyStoreSummary(
  state: TopologyStoreState,
): TopologyStoreSummary {
  return {
    topologyNodeCount: state.topologySnapshot?.nodes.length ?? 0,
    topologyRouteCount: state.topologySnapshot?.routes.length ?? 0,
    trackedPortNodeCount:
      state.topologySnapshot?.nodes.filter((node) => node.ports.length > 0).length ??
      0,
    trackedStatusNodeCount:
      state.topologySnapshot?.nodes.filter((node) => node.online || node.collector_degraded).length ??
      0,
    lastAppliedEventName: state.lastAppliedEventName,
    topologyVersion: state.topologyVersion,
  }
}
