import { useQuery } from '@tanstack/react-query'
import { fetchProxyConfigs } from '../api/proxy-configs'

export function proxyConfigsQueryKey(nodeName: string | null) {
  return ['proxy-configs', nodeName] as const
}

export function useProxyConfigs(nodeName: string | null) {
  return useQuery({
    queryKey: proxyConfigsQueryKey(nodeName),
    queryFn: () => fetchProxyConfigs(nodeName ?? undefined),
    enabled: Boolean(nodeName),
  })
}
