import { pb } from '@/lib/client'
import {
  commandBody,
  readCommand,
  speakerDeleteGroupPayload,
  type Command,
} from '@/lib/commands'
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
    const command = readCommand(record)
    await pb.collection('commands').delete(command.id)
  }
}

function speakerDeleteFailed(command: Command): Error {
  return new Error(command.error === '' ? 'Voiceprint cleanup failed' : command.error)
}

async function deleteSpeakerGroupsForAgent(agentId: string): Promise<void> {
  const groups = await fetchSpeakerGroups(agentId)
  for (const group of groups) {
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
  assertNoDevicesForAgent(await countDevicesForAgent(agentId))
  await deleteSpeakerGroupsForAgent(agentId)
  await deleteCommandsForAgent(agentId)
  await pb.collection('agents').delete(agentId)
}
