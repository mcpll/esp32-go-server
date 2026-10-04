import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { bindDevice, fetchAgents, fetchDevices, setDeviceActivated } from '@/lib/api'
import { agentKeys, deviceKeys } from '@/lib/queryKeys'

export function useDeviceScreen() {
  const queryClient = useQueryClient()
  return useSuspenseQuery({
    queryKey: deviceKeys.screen,
    queryFn: async () => {
      const [agents, devices] = await Promise.all([
        queryClient.ensureQueryData({ queryKey: agentKeys.all, queryFn: fetchAgents }),
        queryClient.ensureQueryData({ queryKey: deviceKeys.all, queryFn: fetchDevices }),
      ])
      return { agents, devices }
    },
  })
}

export function useBindDevice() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: bindDevice,
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: deviceKeys.all }),
        queryClient.invalidateQueries({ queryKey: deviceKeys.screen }),
      ])
    },
  })
}

export function useSetDeviceActivated() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: setDeviceActivated,
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: deviceKeys.all }),
        queryClient.invalidateQueries({ queryKey: deviceKeys.screen }),
      ])
    },
  })
}
