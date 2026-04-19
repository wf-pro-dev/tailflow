import { useQuery } from '@tanstack/react-query'
import { fetchNodes } from '../api/nodes'

export const nodesQueryKey = ['nodes'] as const

export function useNodes() {
  return useQuery({
    queryKey: nodesQueryKey,
    queryFn: fetchNodes,
    select: (nodes) => [...nodes].sort((left, right) => left.name.localeCompare(right.name)),
  })
}
