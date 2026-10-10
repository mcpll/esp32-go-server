import { asJsonObject, isJsonObject, type JsonObject } from '@/lib/json'
import { stripSecrets } from '@/lib/providerForm'

export type Agent = {
  id: string
  name: string
  prompt: string
  asrProvider: string
  asrConfig: JsonObject
  llmProvider: string
  llmConfig: JsonObject
  ttsProvider: string
  ttsConfig: JsonObject
}

export type Device = {
  id: string
  code: string
  note: string
  activated: boolean
  online: boolean
  deviceId: string
  agentId: string
  agentName: string
}

export function readAgent(input: unknown): Agent {
  const record = asRecord(input, 'Agent')
  return {
    id: requiredString(record, 'id', 'Agent'),
    name: requiredString(record, 'name', 'Agent'),
    prompt: requiredString(record, 'prompt', 'Agent'),
    asrProvider: requiredString(record, 'asr_provider', 'Agent'),
    asrConfig: stripSecrets(asJsonObject(record.asr_config, 'asr_config')),
    llmProvider: requiredString(record, 'llm_provider', 'Agent'),
    llmConfig: stripSecrets(asJsonObject(record.llm_config, 'llm_config')),
    ttsProvider: requiredString(record, 'tts_provider', 'Agent'),
    ttsConfig: stripSecrets(asJsonObject(record.tts_config, 'tts_config')),
  }
}

export function readDevice(input: unknown): Device {
  const record = asRecord(input, 'Device')
  return {
    id: requiredString(record, 'id', 'Device'),
    code: requiredString(record, 'code', 'Device'),
    note: optionalString(record, 'note'),
    activated: requiredBool(record, 'activated'),
    online: requiredBool(record, 'online'),
    deviceId: requiredString(record, 'device_id', 'Device'),
    agentId: relationId(record),
    agentName: expandedAgentName(record.expand),
  }
}

export function readAuth(record: unknown): { id: string; email: string } {
  const row = asRecord(record, 'Login')
  return {
    id: requiredString(row, 'id', 'Login'),
    email: requiredString(row, 'email', 'Login'),
  }
}

function asRecord(input: unknown, label: string): { [key: string]: unknown } {
  if (!isJsonObject(input)) throw new Error(`${label} is missing`)
  return input
}

function requiredString(
  record: { [key: string]: unknown },
  key: string,
  label: string,
): string {
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

function requiredBool(record: { [key: string]: unknown }, key: string): boolean {
  const value = record[key]
  if (typeof value !== 'boolean') throw new Error(`${key} must be true or false`)
  return value
}

function relationId(record: { [key: string]: unknown }): string {
  const value = record.agent
  if (value === undefined || value === null || value === '') return ''
  if (typeof value !== 'string') throw new Error('Device agent must be an id')
  return value
}

function expandedAgentName(expand: unknown): string {
  if (expand === undefined || expand === null) return ''
  if (!isJsonObject(expand)) throw new Error('Device agent expand is invalid')
  const agent = expand.agent
  if (agent === undefined || agent === null || agent === '') return ''
  if (!isJsonObject(agent)) throw new Error('Device agent expand is invalid')
  const name = agent.name
  if (typeof name !== 'string') throw new Error('Device agent name is missing')
  return name
}
