import { Link } from 'react-router-dom'
import { Bot } from 'lucide-react'
import { useAgents } from '@/hooks/useAgents'

export function AgentsPage() {
  const { data: agents } = useAgents()
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="flex h-14 shrink-0 items-center border-b border-border px-4">
        <h1 className="text-sm font-semibold">Agents</h1>
      </header>
      <div className="flex-1 overflow-y-auto px-6 py-6">
        {agents.length === 0 ? (
          <p className="text-sm text-muted-foreground">No agents yet.</p>
        ) : (
          <ul className="grid max-w-xl gap-2 sm:grid-cols-2">
            {agents.map((agent) => (
              <li key={agent.id}>
                <Link
                  to={`/agents/${agent.id}`}
                  className="flex items-center gap-3 rounded-2xl border border-border bg-muted/80 px-4 py-3 transition-colors hover:border-primary/30 hover:bg-accent focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
                >
                  <Bot className="size-4 shrink-0 text-primary" />
                  <span className="truncate text-sm font-medium">{agent.name}</span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
