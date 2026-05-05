import { useEffect, useState, useEffectEvent } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import type { TopologyPatch, TopologyReset, TopologyResponse } from '../api/types'
import { fetchTopology } from '../api/topology'
import {
  createEventStream,
  type EventStreamStatus,
  type StreamEvent,
} from './client'
import { healthQueryKey } from '../hooks/useHealth'
import { nodesQueryKey } from '../hooks/useNodes'
import { topologyQueryKey } from '../hooks/useTopology'
import { useTopologyStore } from '../store/topology'

const TOPOLOGY_STREAM_EVENT_NAMES = [
  'topology.snapshot',
  'topology.patch',
  'topology.reset',
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
  const applyTopologyPatch = useTopologyStore((state) => state.applyTopologyPatch)

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

  const handleTopologyPatch = useEffectEvent((event: StreamEvent<TopologyPatch>) => {
    logTopologyEvent(event)
    const currentVersion = useTopologyStore.getState().topologyVersion
    if (currentVersion > 0 && event.data.version !== currentVersion + 1) {
      void queryClient
        .fetchQuery({
          queryKey: topologyQueryKey,
          queryFn: fetchTopology,
        })
        .then((snapshot) => {
          applySnapshot(snapshot)
          queryClient.setQueryData(topologyQueryKey, snapshot)
        })
      setStreamState((current) => ({
        ...current,
        error: null,
        lastEventId: event.lastEventId,
        lastEventName: 'topology.patch.out_of_order',
      }))
      return
    }
    applyTopologyPatch(event.data)
    queryClient.invalidateQueries({ queryKey: healthQueryKey })
    setStreamState((current) => ({
      ...current,
      error: null,
      lastEventId: event.lastEventId,
      lastEventName: event.name,
    }))
  })

  const handleTopologyReset = useEffectEvent((event: StreamEvent<TopologyReset>) => {
    logTopologyEvent(event)
    applySnapshot(event.data.snapshot)
    queryClient.setQueryData(topologyQueryKey, event.data.snapshot)
    void Promise.all([
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
          case 'topology.patch':
            handleTopologyPatch(event as StreamEvent<TopologyPatch>)
            break
          case 'topology.reset':
            handleTopologyReset(event as StreamEvent<TopologyReset>)
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
