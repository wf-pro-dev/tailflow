import { env } from '../env'
import type { ApiErrorResponse } from './types'

export class ApiError extends Error {
  status: number
  hint?: string

  constructor(message: string, status: number, hint?: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.hint = hint
  }
}

function buildUrl(path: string): string {
  if (!env.apiBaseUrl) {
    return path
  }

  const baseUrl = env.apiBaseUrl.endsWith('/')
    ? env.apiBaseUrl.slice(0, -1)
    : env.apiBaseUrl

  return `${baseUrl}${path}`
}

export async function fetchJson<TResponse>(
  path: string,
  init?: RequestInit,
): Promise<TResponse> {
  const response = await fetch(buildUrl(path), {
    headers: {
      'Content-Type': 'application/json',
      ...init?.headers,
    },
    ...init,
  })

  if (!response.ok) {
    let payload: ApiErrorResponse | undefined

    try {
      payload = (await response.json()) as ApiErrorResponse
    } catch {
      payload = undefined
    }

    throw new ApiError(
      payload?.error ?? `Request failed with status ${response.status}`,
      response.status,
      payload?.hint,
    )
  }

  if (response.status === 204) {
    return undefined as TResponse
  }

  return (await response.json()) as TResponse
}
