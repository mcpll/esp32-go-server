import { StatusView } from '@/components/StatusView'
import { usePoolStats } from '@/hooks/usePoolStats'

export function StatusPage() {
  const { data } = usePoolStats()
  return <StatusView stats={data} />
}
