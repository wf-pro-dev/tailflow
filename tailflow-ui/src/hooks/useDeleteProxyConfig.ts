import { useMutation, useQueryClient } from '@tanstack/react-query'
import { deleteProxyConfig } from '../api/proxy-configs'
import { healthQueryKey } from './useHealth'
import { nodesQueryKey } from './useNodes'
import {
  proxyConfigQueryKey,
  proxyConfigsQueryKey,
} from './useProxyConfigs'
import { topologyQueryKey } from './useTopology'

interface DeleteProxyConfigVariables {
  configID: string
  nodeName: string
}

export function useDeleteProxyConfig() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (variables: DeleteProxyConfigVariables) => {
      await deleteProxyConfig(variables.configID)
      return variables
    },
    onSuccess: async (variables) => {
      queryClient.removeQueries({
        queryKey: proxyConfigQueryKey(variables.configID),
      })
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: proxyConfigsQueryKey(variables.nodeName),
        }),
        queryClient.invalidateQueries({ queryKey: topologyQueryKey }),
        queryClient.invalidateQueries({ queryKey: nodesQueryKey }),
        queryClient.invalidateQueries({ queryKey: healthQueryKey }),
      ])
    },
  })
}
