export type SpeakerChatMode = 'off' | 'identified_only'

export type SpeakerSampleStatus = 'pending' | 'enrolled' | 'error'

export type SpeakerGroup = {
  id: string
  name: string
  prompt: string
  voice: string
}

export type SpeakerSample = {
  id: string
  groupId: string
  status: SpeakerSampleStatus
  fileName: string
}

function asRecord(input: unknown, label: string): { [key: string]: unknown } {
  if (input === null || typeof input !== 'object' || Array.isArray(input)) {
    throw new Error(`${label} is missing`)
  }
  return input as { [key: string]: unknown }
}

function requiredString(record: { [key: string]: unknown }, key: string, label: string): string {
  const value = record[key]
  if (typeof value !== 'string' || value.trim() === '') {
    throw new Error(`${label} ${key} is missing`)
  }
  return value
}

function optionalString(record: { [key: string]: unknown }, key: string): string {
  const value = record[key]
  if (value === undefined || value === null || value === '') return ''
  if (typeof value !== 'string') throw new Error(`${key} must be text`)
  return value
}

function relationId(record: { [key: string]: unknown }, key: string): string {
  const value = record[key]
  if (value === undefined || value === null || value === '') return ''
  if (typeof value !== 'string') throw new Error(`${key} must be an id`)
  return value
}

function sampleStatus(value: unknown): SpeakerSampleStatus {
  if (value === 'pending' || value === 'enrolled' || value === 'error') return value
  throw new Error('Sample status is missing')
}

function fileName(record: { [key: string]: unknown }): string {
  const value = record.file
  if (typeof value === 'string' && value !== '') return value
  if (Array.isArray(value) && value.length > 0 && typeof value[0] === 'string') return value[0]
  return ''
}

export function readSpeakerGroup(input: unknown): SpeakerGroup {
  const record = asRecord(input, 'Speaker group')
  return {
    id: requiredString(record, 'id', 'Speaker group'),
    name: requiredString(record, 'name', 'Speaker group'),
    prompt: optionalString(record, 'prompt'),
    voice: optionalString(record, 'voice'),
  }
}

export function readSpeakerSample(input: unknown): SpeakerSample {
  const record = asRecord(input, 'Speaker sample')
  return {
    id: requiredString(record, 'id', 'Speaker sample'),
    groupId: relationId(record, 'group'),
    status: sampleStatus(record.status),
    fileName: fileName(record),
  }
}

export function normalizeSpeakerChatMode(value: unknown): SpeakerChatMode {
  if (value === 'identified_only') return 'identified_only'
  return 'off'
}
