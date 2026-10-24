import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import {
  createSpeakerGroup,
  deleteSpeakerGroupRecord,
  deleteSpeakerSampleRecord,
  fetchSpeakerGroups,
  fetchSpeakerSamples,
  updateSpeakerGroup,
  uploadSpeakerSample,
} from '@/lib/api'
import { speakerKeys } from '@/lib/queryKeys'

export function useSpeakerGroups(agentId: string) {
  return useSuspenseQuery({
    queryKey: speakerKeys.groups(agentId),
    queryFn: () => fetchSpeakerGroups(agentId),
  })
}

export function useSpeakerSamples(groupId: string) {
  return useSuspenseQuery({
    queryKey: speakerKeys.samples(groupId),
    queryFn: () => fetchSpeakerSamples(groupId),
  })
}

export function useCreateSpeakerGroup(agentId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: createSpeakerGroup,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: speakerKeys.groups(agentId) })
    },
  })
}

export function useUpdateSpeakerGroup(agentId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: updateSpeakerGroup,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: speakerKeys.groups(agentId) })
    },
  })
}

export function useDeleteSpeakerGroup(agentId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: deleteSpeakerGroupRecord,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: speakerKeys.groups(agentId) })
    },
  })
}

export function useUploadSpeakerSample(groupId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (file: File) => uploadSpeakerSample(groupId, file),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: speakerKeys.samples(groupId) })
    },
  })
}

export function useDeleteSpeakerSample(groupId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: deleteSpeakerSampleRecord,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: speakerKeys.samples(groupId) })
    },
  })
}
