import { useQuery } from '@tanstack/react-query'
import { fetchRuns } from '../api/runs'

export const runsQueryKey = ['runs'] as const

export function useRuns() {
  return useQuery({
    queryKey: runsQueryKey,
    queryFn: fetchRuns,
    select: (runs) =>
      [...runs].sort(
        (left, right) =>
          new Date(right.finished_at).getTime() - new Date(left.finished_at).getTime(),
      ),
  })
}
