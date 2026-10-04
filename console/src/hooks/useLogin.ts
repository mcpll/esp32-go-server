import { useMutation } from '@tanstack/react-query'
import { pb } from '@/lib/client'

export function useLogin() {
  return useMutation({
    mutationFn: async (input: { email: string; password: string }) => {
      const email = input.email.trim()
      if (email === '' || input.password === '') {
        throw new Error('Email and password are required')
      }
      await pb.collection('users').authWithPassword(email, input.password)
    },
  })
}
