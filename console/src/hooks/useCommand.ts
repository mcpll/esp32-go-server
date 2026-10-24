import { useCallback, useEffect, useRef, useState } from 'react'
import { createCommand, waitForCommand } from '@/lib/api'
import { pb } from '@/lib/client'
import { readCommand, type Command } from '@/lib/commands'
import { errorMessage } from '@/lib/errors'
import type { JsonObject } from '@/lib/json'

export function useCommand() {
  const [command, setCommand] = useState<Command | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [sending, setSending] = useState(false)
  const unsubscribe = useRef<(() => Promise<void>) | null>(null)

  useEffect(() => {
    return () => {
      void unsubscribe.current?.()
    }
  }, [])

  const send = useCallback(async (body: JsonObject): Promise<Command | null> => {
    setSending(true)
    setError(null)
    try {
      if (unsubscribe.current !== null) {
        await unsubscribe.current()
        unsubscribe.current = null
      }
      const created = await createCommand(body)
      setCommand(created)
      unsubscribe.current = await pb.collection('commands').subscribe(created.id, (event) => {
        try {
          setCommand(readCommand(event.record))
        } catch (caught) {
          setError(errorMessage(caught))
        }
      })
      const finished = await waitForCommand(created.id)
      setCommand(finished)
      if (finished.status === 'error') {
        setError(finished.error === '' ? 'Command failed' : finished.error)
      }
      return finished
    } catch (caught) {
      setError(errorMessage(caught))
      return null
    } finally {
      setSending(false)
    }
  }, [])

  return { command, error, sending, send }
}
