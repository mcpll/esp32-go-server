import { Link } from 'react-router-dom'
import { useAgents } from '@/hooks/useAgents'

export function AgentsPage() {
  const { data: agents } = useAgents()
  return (
    <section className="grid gap-4">
      <h1 className="text-xl font-semibold">Agents</h1>
      {agents.length === 0 ? (
        <p>No agents yet.</p>
      ) : (
        <ul className="grid gap-2">
          {agents.map((agent) => (
            <li key={agent.id}>
              <Link className="font-medium underline-offset-4 hover:underline" to={`/agents/${agent.id}`}>
                {agent.name}
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
