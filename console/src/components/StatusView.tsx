import type { PoolStats } from '@/lib/poolStats'

export function StatusView({ stats }: { stats: PoolStats | null }) {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="flex h-14 shrink-0 items-center border-b border-border px-4">
        <h1 className="text-sm font-semibold">Status</h1>
      </header>
      <div className="flex-1 overflow-y-auto px-6 py-6">
        {stats === null || stats.pools.length === 0 ? (
          <p className="max-w-sm text-sm text-muted-foreground">Nothing has been reported yet.</p>
        ) : (
          <ul className="grid max-w-lg gap-2">
            {stats.pools.map((pool) => (
              <li key={pool.key} className="rounded-2xl border border-border bg-muted/50 px-4 py-3">
                <h2 className="text-sm text-muted-foreground">{pool.kind}</h2>
                <p className="text-3xl font-semibold tracking-tight text-foreground">{pool.inUse} in use</p>
                <p className="text-sm text-muted-foreground">
                  {pool.available} idle, {pool.total} total
                </p>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
