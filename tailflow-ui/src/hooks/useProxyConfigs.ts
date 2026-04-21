import { useQuery } from '@tanstack/react-query'
import { fetchProxyConfig, fetchProxyConfigs } from '../api/proxy-configs'

export function proxyConfigsQueryKey(nodeName: string | null) {
  return ['proxy-configs', nodeName] as const
}

export function proxyConfigQueryKey(configID: string | null) {
  return ['proxy-config', configID] as const
}

export function useProxyConfigs(nodeName: string | null) {
  return useQuery({
    queryKey: proxyConfigsQueryKey(nodeName),
    queryFn: () => fetchProxyConfigs(nodeName ?? undefined),
    enabled: Boolean(nodeName),
  })
}

export function useProxyConfig(configID: string | null) {
  return useQuery({
    queryKey: proxyConfigQueryKey(configID),
    queryFn: () => fetchProxyConfig(configID ?? ''),
    enabled: Boolean(configID),
  })
}
