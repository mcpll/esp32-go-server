import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { fetchSettings, saveSetting } from '@/lib/api'
import { settingKeys } from '@/lib/queryKeys'

export function useSettings() {
  return useSuspenseQuery({
    queryKey: settingKeys.all,
    queryFn: fetchSettings,
  })
}

export function useSaveSetting() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: saveSetting,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: settingKeys.all })
    },
  })
}
