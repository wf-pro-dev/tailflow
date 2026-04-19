import { fetchJson } from './client'
import type {
  ParsedProxyConfigResponse,
  ProxyConfigInput,
  SetProxyConfigRequest,
  SetProxyConfigResponse,
} from './types'

export function fetchProxyConfigs(nodeName?: string): Promise<ProxyConfigInput[]> {
  const params = nodeName
    ? `?node=${encodeURIComponent(nodeName)}`
    : ''

  return fetchJson<ProxyConfigInput[]>(`/api/v1/configs${params}`)
}

export function fetchProxyConfig(configID: string): Promise<ParsedProxyConfigResponse> {
  return fetchJson<ParsedProxyConfigResponse>(`/api/v1/configs/${configID}`)
}

export function setProxyConfig(
  nodeName: string,
  request: SetProxyConfigRequest,
): Promise<SetProxyConfigResponse> {
  return fetchJson<SetProxyConfigResponse>(`/api/v1/configs/${nodeName}`, {
    method: 'PUT',
    body: JSON.stringify(request),
  })
}

export function deleteProxyConfig(configID: string): Promise<void> {
  return fetchJson<void>(`/api/v1/configs/${configID}`, {
    method: 'DELETE',
  })
}
