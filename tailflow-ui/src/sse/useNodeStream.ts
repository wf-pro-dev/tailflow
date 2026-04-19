import { useEffect, useState, useEffectEvent } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import type {
  NodeResponse,
  NodeStatusEvent,
  PortBoundEvent,
  PortReleasedEvent,
  SnapshotEvent,
} from '../api/types'
import {
  createEventStream,
  type EventStreamStatus,
  type StreamEvent,
} from './client'
import { nodesQueryKey } from '../hooks/useNodes'
import { useTopologyStore } from '../store/topology'

const NODE_STREAM_EVENT_NAMES = [
  'nodes.snapshot',
  'snapshot.updated',
  'port.bound',
  'port.released',
  'node.connected',
  'node.disconnected',
  'node.degraded',
] as const

interface NodeStreamState {
  status: EventStreamStatus
  error: string | null
  lastEventId: string | null
  lastEventName: string | null
}

function logNodeEvent<TPayload>(event: StreamEvent<TPayload>) {
  console.debug(`[tailflow:sse:nodes] ${event.name}`, event.data)
}

export function useNodeStream(): NodeStreamState {
  const queryClient = useQueryClient()
  const [streamState, setStreamState] = useState<NodeStreamState>({
    status: 'connecting',
    error: null,
    lastEventId: null,
    lastEventName: null,
  })
  const applyNodesSnapshot = useTopologyStore((state) => state.applyNodesSnapshot)
  const applySnapshotUpdated = useTopologyStore(
    (state) => state.applySnapshotUpdated,
  )
  const applyPortBound = useTopologyStore((state) => state.applyPortBound)
  const applyPortReleased = useTopologyStore((state) => state.applyPortReleased)
  const applyNodeStatus = useTopologyStore((state) => state.applyNodeStatus)

  const handleNodesSnapshot = useEffectEvent((event: StreamEvent<NodeResponse[]>) => {
    logNodeEvent(event)
    applyNodesSnapshot(event.data)
    queryClient.setQueryData(nodesQueryKey, event.data)
    setStreamState((current) => ({
      ...current,
      error: null,
      lastEventId: event.lastEventId,
      lastEventName: event.name,
    }))
  })

  const handleSnapshotUpdated = useEffectEvent((event: StreamEvent<SnapshotEvent>) => {
    logNodeEvent(event)
    applySnapshotUpdated(event.data)
    setStreamState((current) => ({
      ...current,
      error: null,
      lastEventId: event.lastEventId,
      lastEventName: event.name,
    }))
  })

  const handlePortBound = useEffectEvent((event: StreamEvent<PortBoundEvent>) => {
    logNodeEvent(event)
    applyPortBound(event.data)
    setStreamState((current) => ({
      ...current,
      error: null,
      lastEventId: event.lastEventId,
      lastEventName: event.name,
    }))
  })

  const handlePortReleased = useEffectEvent((event: StreamEvent<PortReleasedEvent>) => {
    logNodeEvent(event)
    applyPortReleased(event.data)
    setStreamState((current) => ({
      ...current,
      error: null,
      lastEventId: event.lastEventId,
      lastEventName: event.name,
    }))
  })

  const handleNodeStatusChanged = useEffectEvent((event: StreamEvent<NodeStatusEvent>) => {
    logNodeEvent(event)
    applyNodeStatus(event.name, event.data)
    setStreamState((current) => ({
      ...current,
      error: null,
      lastEventId: event.lastEventId,
      lastEventName: event.name,
    }))
  })

  useEffect(() => {
    const stream = createEventStream({
      path: '/api/v1/nodes/stream',
      eventNames: [...NODE_STREAM_EVENT_NAMES],
      onOpen: () => {
        console.info('[tailflow:sse:nodes] connected')
      },
      onStatusChange: (status) => {
        setStreamState((current) => ({
          ...current,
          status,
        }))
      },
      onError: (error) => {
        console.warn('[tailflow:sse:nodes] reconnecting', error)
        setStreamState((current) => ({
          ...current,
          status: 'reconnecting',
          error: `Reconnect attempt ${error.attempt}`,
        }))
      },
      onEvent: (event) => {
        switch (event.name) {
          case 'nodes.snapshot':
            handleNodesSnapshot(event as StreamEvent<NodeResponse[]>)
            break
          case 'snapshot.updated':
            handleSnapshotUpdated(event as StreamEvent<SnapshotEvent>)
            break
          case 'port.bound':
            handlePortBound(event as StreamEvent<PortBoundEvent>)
            break
          case 'port.released':
            handlePortReleased(event as StreamEvent<PortReleasedEvent>)
            break
          case 'node.connected':
          case 'node.disconnected':
          case 'node.degraded':
            handleNodeStatusChanged(event as StreamEvent<NodeStatusEvent>)
            break
          default:
            console.debug('[tailflow:sse:nodes] unhandled event', event)
        }
      },
    })

    return () => {
      stream.close()
    }
  }, [queryClient])

  return streamState
}
