import { ClientResponseError } from 'pocketbase'
import { isJsonObject } from '@/lib/json'

export function errorMessage(error: unknown): string {
  if (error instanceof ClientResponseError) {
    const response: unknown = error.response
    if (isJsonObject(response) && typeof response.message === 'string' && response.message !== '') {
      return response.message
    }
  }
  if (error instanceof Error && error.message !== '') return error.message
  return 'Something went wrong'
}
