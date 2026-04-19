import { useQuery } from '@tanstack/react-query'
import { fetchHealth } from '../api/health'

export const healthQueryKey = ['health'] as const

export function useHealth() {
  return useQuery({
    queryKey: healthQueryKey,
    queryFn: fetchHealth,
    refetchInterval: 30_000,
  })
}
