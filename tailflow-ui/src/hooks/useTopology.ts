import { useQuery } from '@tanstack/react-query'
import { fetchTopology } from '../api/topology'

export const topologyQueryKey = ['topology'] as const

export function useTopology() {
  return useQuery({
    queryKey: topologyQueryKey,
    queryFn: fetchTopology,
  })
}
