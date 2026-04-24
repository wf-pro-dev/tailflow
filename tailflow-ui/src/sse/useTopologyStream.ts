import { useEffect, useState, useEffectEvent } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import type { CollectionRun, TopologyResponse } from '../api/types'
import {
  createEventStream,
  type EventStreamStatus,
  type StreamEvent,
} from './client'
import { healthQueryKey } from '../hooks/useHealth'
import { nodesQueryKey } from '../hooks/useNodes'
import { runsQueryKey } from '../hooks/useRuns'
import { topologyQueryKey } from '../hooks/useTopology'
import { useTopologyStore } from '../store/topology'

const TOPOLOGY_STREAM_EVENT_NAMES = [
  'topology.snapshot',
  'topology.run_completed',
] as const

interface TopologyStreamState {
  status: EventStreamStatus
  error: string | null
  lastEventId: string | null
  lastEventName: string | null
}

function logTopologyEvent<TPayload>(event: StreamEvent<TPayload>) {
  console.debug(`[tailflow:sse:topology] ${event.name}`, event.data)
}

export function useTopologyStream(): TopologyStreamState {
  const queryClient = useQueryClient()
  const [streamState, setStreamState] = useState<TopologyStreamState>({
    status: 'connecting',
    error: null,
    lastEventId: null,
    lastEventName: null,
  })
  const applySnapshot = useTopologyStore((state) => state.applySnapshot)

  const handleTopologySnapshot = useEffectEvent(
    (event: StreamEvent<TopologyResponse>) => {
      logTopologyEvent(event)
      applySnapshot(event.data)
      queryClient.setQueryData(topologyQueryKey, event.data)
      setStreamState((current) => ({
        ...current,
        error: null,
        lastEventId: event.lastEventId,
        lastEventName: event.name,
      }))
    },
  )

  const handleRunCompleted = useEffectEvent((event: StreamEvent<CollectionRun>) => {
    logTopologyEvent(event)
    void Promise.all([
      queryClient.invalidateQueries({ queryKey: topologyQueryKey }),
      queryClient.invalidateQueries({ queryKey: runsQueryKey }),
      queryClient.invalidateQueries({ queryKey: nodesQueryKey }),
      queryClient.invalidateQueries({ queryKey: healthQueryKey }),
    ])
    setStreamState((current) => ({
      ...current,
      error: null,
      lastEventId: event.lastEventId,
      lastEventName: event.name,
    }))
  })

  useEffect(() => {
    const stream = createEventStream({
      path: '/api/v1/topology/stream',
      eventNames: [...TOPOLOGY_STREAM_EVENT_NAMES],
      onOpen: () => {
        console.info('[tailflow:sse:topology] connected')
      },
      onStatusChange: (status) => {
        setStreamState((current) => ({
          ...current,
          status,
        }))
      },
      onError: (error) => {
        console.warn('[tailflow:sse:topology] reconnecting', error)
        setStreamState((current) => ({
          ...current,
          status: 'reconnecting',
          error: `Reconnect attempt ${error.attempt}`,
        }))
      },
      onEvent: (event) => {
        switch (event.name) {
          case 'topology.snapshot':
            handleTopologySnapshot(event as StreamEvent<TopologyResponse>)
            break
          case 'topology.run_completed':
            handleRunCompleted(event as StreamEvent<CollectionRun>)
            break
          default:
            console.debug('[tailflow:sse:topology] unhandled event', event)
        }
      },
    })

    return () => {
      stream.close()
    }
  }, [queryClient])

  return streamState
}
