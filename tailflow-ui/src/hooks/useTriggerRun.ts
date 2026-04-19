import { useMutation, useQueryClient } from '@tanstack/react-query'
import { triggerRun } from '../api/runs'
import { healthQueryKey } from './useHealth'
import { nodesQueryKey } from './useNodes'
import { runsQueryKey } from './useRuns'

export function useTriggerRun() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: triggerRun,
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: runsQueryKey }),
        queryClient.invalidateQueries({ queryKey: nodesQueryKey }),
        queryClient.invalidateQueries({ queryKey: healthQueryKey }),
      ])
    },
  })
}
