import { pb } from '@/lib/client'
import { asJsonObject } from '@/lib/json'

async function deleteCommandsForDevice(deviceId: string): Promise<void> {
  const records: unknown = await pb.collection('commands').getFullList({
    filter: pb.filter('device = {:device}', { device: deviceId }),
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

export async function deleteDevice(deviceId: string): Promise<void> {
  await deleteCommandsForDevice(deviceId)
  await pb.collection('devices').delete(deviceId)
}
