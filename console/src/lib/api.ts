import { ClientResponseError } from 'pocketbase'
import { activationUpdate, bindDeviceUpdate, parseDeviceCode } from '@/lib/deviceBind'
import { pb } from '@/lib/client'
import { agentUpdate, type AgentDraft } from '@/lib/providerForm'
import { readAgent, readDevice, type Agent, type Device } from '@/lib/records'
import { readSetting, type Setting } from '@/lib/settingsForm'
import type { JsonObject } from '@/lib/json'

export async function fetchAgents(): Promise<Agent[]> {
  const records: unknown = await pb.collection('agents').getFullList({
    sort: 'name',
    fields: 'id,name,prompt,asr_provider,asr_config,llm_provider,llm_config,tts_provider,tts_config',
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
    fields: 'id,code,note,activated,online,agent,expand.agent.name',
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

export async function setDeviceActivated(input: {
  id: string
  activated: boolean
  agentId: string
}): Promise<void> {
  await pb.collection('devices').update(input.id, activationUpdate(input.activated, input.agentId))
}
