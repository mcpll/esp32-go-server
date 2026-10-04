import { useQueryClient } from '@tanstack/react-query'
import { useMemo, type ReactNode } from 'react'
import { pb } from '@/lib/client'
import { readAuth } from '@/lib/records'
import { SessionContext, type SessionContextValue } from '@/session/session-context'

export function SessionProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()
  const record = pb.authStore.record
  const logout = useMemo(() => {
    return () => {
      queryClient.clear()
      pb.authStore.clear()
    }
  }, [queryClient])
  const value = useMemo<SessionContextValue | null>(() => {
    if (record === null) return null
    const auth = readAuth(record)
    return {
      state: { email: auth.email },
      actions: { logout },
      meta: { recordId: auth.id },
    }
  }, [logout, record])

  if (value === null) return null
  return <SessionContext value={value}>{children}</SessionContext>
}
