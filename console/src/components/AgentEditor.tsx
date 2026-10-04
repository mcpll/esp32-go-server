import { useParams } from 'react-router-dom'
import { AgentForm } from '@/components/AgentForm'

export function AgentEditor() {
  const { id } = useParams()
  if (id === undefined || id === '') throw new Error('Missing agent')
  return <AgentForm key={id} id={id} />
}
