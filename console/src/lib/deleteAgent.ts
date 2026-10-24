import { pb } from '@/lib/client'
import { commandBody, speakerDeleteGroupPayload, type Command } from '@/lib/commands'
import { asJsonObject } from '@/lib/json'
import {
  createCommand,
  deleteSpeakerGroupRecord,
  fetchSpeakerGroups,
  waitForCommand,
} from '@/lib/api'
import { assertNoDevicesForAgent } from '@/lib/deleteAgentPreflight'

async function countDevicesForAgent(agentId: string): Promise<number> {
  const records: unknown = await pb.collection('devices').getFullList({
    filter: pb.filter('agent = {:agent}', { agent: agentId }),
    fields: 'id',
  })
  if (!Array.isArray(records)) throw new Error('Device list is not an array')
  return records.length
}

async function deleteCommandsForAgent(agentId: string): Promise<void> {
  const records: unknown = await pb.collection('commands').getFullList({
    filter: pb.filter('agent = {:agent}', { agent: agentId }),
    fields: 'id',
  })
  if (!Array.isArray(records)) throw new Error('Command list is not an array')
  for (const record of records) {
    const row = asJsonObject(record, 'Command')
    const id = row.id
    if (typeof id !== 'string' || id.trim() === '') throw new Error('Command id is missing')
    await pb.collection('commands').delete(id)
  }
}

function speakerDeleteFailed(command: Command): Error {
  return new Error(command.error === '' ? 'Voiceprint cleanup failed' : command.error)
}

async function assertNoDevicesBound(agentId: string): Promise<void> {
  assertNoDevicesForAgent(await countDevicesForAgent(agentId))
}

async function deleteSpeakerGroupsForAgent(agentId: string): Promise<void> {
  for (;;) {
    await assertNoDevicesBound(agentId)
    const groups = await fetchSpeakerGroups(agentId)
    if (groups.length === 0) break
    const group = groups[0]
    const created = await createCommand(
      commandBody({
        type: 'speaker_delete',
        agentId,
        payload: speakerDeleteGroupPayload(group.id),
      }),
    )
    const finished = await waitForCommand(created.id)
    if (finished.status !== 'done') throw speakerDeleteFailed(finished)
    await deleteSpeakerGroupRecord(group.id)
  }
}

export async function deleteAgent(agentId: string): Promise<void> {
  await assertNoDevicesBound(agentId)
  await deleteSpeakerGroupsForAgent(agentId)
  await assertNoDevicesBound(agentId)
  await deleteCommandsForAgent(agentId)
  await assertNoDevicesBound(agentId)
  await pb.collection('agents').delete(agentId)
}
