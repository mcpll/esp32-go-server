import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { fetchAgent, fetchAgents, saveAgent } from '@/lib/api'
import { agentKeys, deviceKeys } from '@/lib/queryKeys'

export function useAgents() {
  return useSuspenseQuery({
    queryKey: agentKeys.all,
    queryFn: fetchAgents,
  })
}

export function useAgent(id: string) {
  return useSuspenseQuery({
    queryKey: agentKeys.detail(id),
    queryFn: () => fetchAgent(id),
  })
}

export function useSaveAgent() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: saveAgent,
    onSuccess: async (_result, variables) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: agentKeys.all }),
        queryClient.invalidateQueries({ queryKey: agentKeys.detail(variables.id) }),
        queryClient.invalidateQueries({ queryKey: deviceKeys.screen }),
      ])
    },
  })
}
