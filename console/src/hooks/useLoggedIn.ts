import { useSyncExternalStore } from 'react'
import { pb } from '@/lib/client'

export function useLoggedIn(): boolean {
  return useSyncExternalStore(
    (onStoreChange) => pb.authStore.onChange(onStoreChange),
    () => pb.authStore.isValid,
  )
}
