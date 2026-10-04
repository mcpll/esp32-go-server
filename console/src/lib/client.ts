import { QueryClient } from '@tanstack/react-query'
import PocketBase, { ClientResponseError } from 'pocketbase'

export const pb = new PocketBase(window.location.origin)
pb.autoCancellation(false)

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false },
  },
})

let dropping = false

function dropSession(error: unknown): void {
  if (dropping) return
  if (!(error instanceof ClientResponseError) || error.status !== 401 || !pb.authStore.isValid) {
    return
  }
  dropping = true
  pb.authStore.clear()
  queryClient.clear()
  dropping = false
}

queryClient.getQueryCache().subscribe((event) => {
  dropSession(event.query.state.error)
})

queryClient.getMutationCache().subscribe((event) => {
  if (event.mutation === undefined) return
  dropSession(event.mutation.state.error)
})
