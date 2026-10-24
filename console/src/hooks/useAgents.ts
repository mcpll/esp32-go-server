import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { fetchAgent, fetchAgents, saveAgent } from '@/lib/api'
import { deleteAgent } from '@/lib/deleteAgent'
import { agentKeys, deviceKeys, speakerKeys } from '@/lib/queryKeys'

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

export function useDeleteAgent() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: deleteAgent,
    onSettled: async (_result, error, agentId) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: agentKeys.all }),
        queryClient.invalidateQueries({ queryKey: agentKeys.detail(agentId) }),
        queryClient.invalidateQueries({ queryKey: deviceKeys.screen }),
        queryClient.invalidateQueries({ queryKey: speakerKeys.groups(agentId) }),
      ])
      if (error === null) {
        queryClient.removeQueries({ queryKey: agentKeys.detail(agentId) })
        queryClient.removeQueries({ queryKey: speakerKeys.groups(agentId) })
      }
    },
  })
}
