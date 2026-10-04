import { use } from 'react'
import { SessionContext, type SessionContextValue } from '@/session/session-context'

export function useSession(): SessionContextValue {
  const value = use(SessionContext)
  if (value === null) throw new Error('Session is missing')
  return value
}
