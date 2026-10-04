import { createContext } from 'react'

export interface SessionState {
  email: string
}

export interface SessionActions {
  logout: () => void
}

export interface SessionMeta {
  recordId: string
}

export interface SessionContextValue {
  state: SessionState
  actions: SessionActions
  meta: SessionMeta
}

export const SessionContext = createContext<SessionContextValue | null>(null)
