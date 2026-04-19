import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { SetProxyConfigRequest } from '../api/types'
import { setProxyConfig } from '../api/proxy-configs'
import { triggerRun } from '../api/runs'
import { healthQueryKey } from './useHealth'
import { nodesQueryKey } from './useNodes'
import { proxyConfigsQueryKey } from './useProxyConfigs'
import { runsQueryKey } from './useRuns'
import { topologyQueryKey } from './useTopology'

interface SetProxyConfigVariables {
  nodeName: string
  request: SetProxyConfigRequest
}

export function useSetProxyConfig() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (variables: SetProxyConfigVariables) => {
      const response = await setProxyConfig(variables.nodeName, variables.request)
      await triggerRun()
      return response
    },
    onSuccess: async (_, variables) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: proxyConfigsQueryKey(variables.nodeName) }),
        queryClient.invalidateQueries({ queryKey: topologyQueryKey }),
        queryClient.invalidateQueries({ queryKey: runsQueryKey }),
        queryClient.invalidateQueries({ queryKey: nodesQueryKey }),
        queryClient.invalidateQueries({ queryKey: healthQueryKey }),
      ])
    },
  })
}
