import { env } from '../env'

export type EventStreamStatus =
  | 'connecting'
  | 'open'
  | 'reconnecting'
  | 'closed'

export interface StreamEvent<TPayload = unknown> {
  name: string
  data: TPayload
  lastEventId: string | null
  rawEvent: MessageEvent<string>
}

export interface EventStreamError {
  attempt: number
  lastEventId: string | null
  rawEvent: Event
}

export interface EventStreamController {
  close: () => void
  getLastEventId: () => string | null
}

interface EventStreamOptions {
  path: string
  eventNames: string[]
  reconnectDelayMs?: number
  onOpen?: () => void
  onError?: (error: EventStreamError) => void
  onStatusChange?: (status: EventStreamStatus) => void
  onEvent?: (event: StreamEvent) => void
}

function buildStreamUrl(path: string, lastEventId: string | null): string {
  if (!env.apiBaseUrl) {
    const url = new URL(path, window.location.origin)
    if (lastEventId) {
      url.searchParams.set('lastEventId', lastEventId)
    }
    return `${url.pathname}${url.search}`
  }

  const normalizedBaseUrl = env.apiBaseUrl.endsWith('/')
    ? env.apiBaseUrl
    : `${env.apiBaseUrl}/`

  const url = new URL(path.replace(/^\//, ''), normalizedBaseUrl)
  if (lastEventId) {
    url.searchParams.set('lastEventId', lastEventId)
  }
  return url.toString()
}

function parseEventData(rawEvent: MessageEvent<string>): unknown {
  if (!rawEvent.data) {
    return null
  }

  return JSON.parse(rawEvent.data) as unknown
}

export function createEventStream(
  options: EventStreamOptions,
): EventStreamController {
  let eventSource: EventSource | null = null
  let reconnectTimer: number | null = null
  let closed = false
  let attempt = 0
  let lastEventId: string | null = null

  const reconnectDelayMs =
    options.reconnectDelayMs ?? env.sseReconnectDelayMs

  const setStatus = (status: EventStreamStatus) => {
    options.onStatusChange?.(status)
  }

  const clearReconnectTimer = () => {
    if (reconnectTimer !== null) {
      window.clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
  }

  const disconnectSource = () => {
    if (eventSource) {
      eventSource.close()
      eventSource = null
    }
  }

  const scheduleReconnect = (rawEvent: Event) => {
    if (closed || reconnectTimer !== null) {
      return
    }

    attempt += 1
    setStatus('reconnecting')
    options.onError?.({
      attempt,
      lastEventId,
      rawEvent,
    })

    disconnectSource()
    reconnectTimer = window.setTimeout(() => {
      reconnectTimer = null
      connect()
    }, reconnectDelayMs)
  }

  const handleStreamEvent = (name: string, rawEvent: MessageEvent<string>) => {
    if (rawEvent.lastEventId) {
      lastEventId = rawEvent.lastEventId
    }

    try {
      options.onEvent?.({
        name,
        data: parseEventData(rawEvent),
        lastEventId,
        rawEvent,
      })
    } catch (error) {
      console.error(`[tailflow:sse] failed to parse ${name}`, error)
    }
  }

  const connect = () => {
    if (closed) {
      return
    }

    setStatus(attempt > 0 ? 'reconnecting' : 'connecting')

    eventSource = new EventSource(buildStreamUrl(options.path, lastEventId))
    eventSource.onopen = () => {
      attempt = 0
      setStatus('open')
      options.onOpen?.()
    }
    eventSource.onerror = (rawEvent) => {
      scheduleReconnect(rawEvent)
    }

    for (const eventName of options.eventNames) {
      eventSource.addEventListener(eventName, (rawEvent) => {
        handleStreamEvent(eventName, rawEvent as MessageEvent<string>)
      })
    }
  }

  connect()

  return {
    close: () => {
      closed = true
      clearReconnectTimer()
      disconnectSource()
      setStatus('closed')
    },
    getLastEventId: () => lastEventId,
  }
}
