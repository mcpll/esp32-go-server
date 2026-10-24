import { useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { useEffect } from 'react'
import { fetchPoolStats, subscribePoolStats } from '@/lib/api'
import { poolStatsAfterEvent, type PoolStats } from '@/lib/poolStats'
import { poolStatsKeys } from '@/lib/queryKeys'

export function usePoolStats() {
  const queryClient = useQueryClient()
  const query = useSuspenseQuery({
    queryKey: poolStatsKeys.main,
    queryFn: fetchPoolStats,
  })

  useEffect(() => {
    let cancelled = false
    let stop: (() => Promise<void>) | undefined
    void subscribePoolStats((event) => {
      queryClient.setQueryData<PoolStats | null>(poolStatsKeys.main, (current) =>
        poolStatsAfterEvent(current ?? null, event),
      )
    })
      .then((unsub) => {
        if (cancelled) {
          void unsub()
          return
        }
        stop = unsub
      })
      .catch(() => {
        // The record loaded by the query stays on screen. A later visit subscribes again.
      })
    return () => {
      cancelled = true
      if (stop !== undefined) void stop()
    }
  }, [queryClient])

  return query
}
