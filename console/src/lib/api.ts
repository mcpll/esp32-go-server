import { ClientResponseError } from 'pocketbase'
import { activationUpdate, bindDeviceUpdate, parseDeviceCode } from '@/lib/deviceBind'
import { pb } from '@/lib/client'
import { agentUpdate, type AgentDraft } from '@/lib/providerForm'
import { readCommand, type Command } from '@/lib/commands'
import { readAgent, readDevice, type Agent, type Device } from '@/lib/records'
import { readSetting, type Setting } from '@/lib/settingsForm'
import { readSpeakerGroup, readSpeakerSample, type SpeakerGroup } from '@/lib/speakers'
import type { JsonObject } from '@/lib/json'

export async function fetchAgents(): Promise<Agent[]> {
  const records: unknown = await pb.collection('agents').getFullList({
    sort: 'name',
    fields:
      'id,name,prompt,asr_provider,asr_config,llm_provider,llm_config,tts_provider,tts_config,voiceprint_enabled,speaker_chat_mode',
  })
  if (!Array.isArray(records)) throw new Error('Agent list is not an array')
  return records.map(readAgent)
}

export async function fetchAgent(id: string): Promise<Agent> {
  const record: unknown = await pb.collection('agents').getOne(id)
  return readAgent(record)
}

export async function saveAgent(input: { id: string } & AgentDraft): Promise<void> {
  const { id, ...draft } = input
  await pb.collection('agents').update(id, agentUpdate(draft))
}

export async function fetchDevices(): Promise<Device[]> {
  const records: unknown = await pb.collection('devices').getFullList({
    sort: 'code',
    expand: 'agent',
    fields: 'id,code,note,activated,online,device_id,agent,expand.agent.name',
  })
  if (!Array.isArray(records)) throw new Error('Device list is not an array')
  return records.map(readDevice)
}

export async function bindDevice(input: {
  code: string
  agentId: string
  note: string
}): Promise<void> {
  const code = parseDeviceCode(input.code)
  const patch = bindDeviceUpdate(input.agentId, input.note)
  let id = ''
  try {
    const found: unknown = await pb.collection('devices').getFirstListItem(
      pb.filter('code = {:code}', { code }),
    )
    id = readDevice(found).id
  } catch (error) {
    if (error instanceof ClientResponseError && error.status === 404) {
      throw new Error('No device is waiting with that code')
    }
    throw error
  }
  await pb.collection('devices').update(id, patch)
}

export async function fetchSettings(): Promise<Setting[]> {
  const records: unknown = await pb.collection('settings').getFullList({ sort: 'key' })
  if (!Array.isArray(records)) throw new Error('Settings list is not an array')
  return records.map(readSetting)
}

export async function saveSetting(input: { id: string; value: JsonObject }): Promise<void> {
  await pb.collection('settings').update(input.id, { value: input.value })
}

export async function createCommand(body: JsonObject): Promise<Command> {
  const created: unknown = await pb.collection('commands').create(body)
  return readCommand(created)
}

export async function fetchCommand(id: string): Promise<Command> {
  const record: unknown = await pb.collection('commands').getOne(id)
  return readCommand(record)
}

export async function waitForCommand(id: string, timeoutMs = 60_000): Promise<Command> {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    const command = await fetchCommand(id)
    if (command.status === 'done' || command.status === 'error') return command
    if (Date.now() > deadline) throw new Error('Command timed out')
    await new Promise((resolve) => setTimeout(resolve, 300))
  }
}

export async function fetchSpeakerGroups(agentId: string): Promise<SpeakerGroup[]> {
  const records: unknown = await pb.collection('speaker_groups').getFullList({
    filter: pb.filter('agent = {:agent}', { agent: agentId }),
    sort: 'name',
    fields: 'id,name,prompt,voice',
  })
  if (!Array.isArray(records)) throw new Error('Speaker group list is not an array')
  return records.map(readSpeakerGroup)
}

export async function createSpeakerGroup(input: {
  agentId: string
  name: string
  prompt: string
  voice: string
}): Promise<SpeakerGroup> {
  const name = input.name.trim()
  if (name === '') throw new Error('Group name is required')
  const record: unknown = await pb.collection('speaker_groups').create({
    agent: input.agentId,
    name,
    prompt: input.prompt.trim(),
    voice: input.voice.trim(),
  })
  return readSpeakerGroup(record)
}

export async function updateSpeakerGroup(input: {
  id: string
  name: string
  prompt: string
  voice: string
}): Promise<void> {
  const name = input.name.trim()
  if (name === '') throw new Error('Group name is required')
  await pb.collection('speaker_groups').update(input.id, {
    name,
    prompt: input.prompt.trim(),
    voice: input.voice.trim(),
  })
}

export async function deleteSpeakerGroupRecord(id: string): Promise<void> {
  await pb.collection('speaker_groups').delete(id)
}

export async function fetchSpeakerSamples(groupId: string) {
  const records: unknown = await pb.collection('speaker_samples').getFullList({
    filter: pb.filter('group = {:group}', { group: groupId }),
    sort: '-created',
    fields: 'id,group,status,file',
  })
  if (!Array.isArray(records)) throw new Error('Speaker sample list is not an array')
  return records.map(readSpeakerSample)
}

export async function uploadSpeakerSample(groupId: string, file: File) {
  const body = new FormData()
  body.append('group', groupId)
  body.append('status', 'pending')
  body.append('file', file)
  const record: unknown = await pb.collection('speaker_samples').create(body)
  return readSpeakerSample(record)
}

export async function deleteSpeakerSampleRecord(id: string): Promise<void> {
  await pb.collection('speaker_samples').delete(id)
}

export async function setDeviceActivated(input: {
  id: string
  activated: boolean
  agentId: string
}): Promise<void> {
  await pb.collection('devices').update(input.id, activationUpdate(input.activated, input.agentId))
}
