import { isJsonObject, type JsonObject } from '@/lib/json'

export type CommandType = 'inject_msg' | 'provider_test' | 'settings_reload'

export type CommandStatus = 'pending' | 'running' | 'done' | 'error'

export type Command = {
  id: string
  status: CommandStatus
  error: string
}

const STATUSES: readonly CommandStatus[] = ['pending', 'running', 'done', 'error']

export function injectPayload(deviceMac: string, message: string, speak: boolean): JsonObject {
  return {
    device: deviceMac,
    message,
    skip_llm: speak,
    auto_listen: false,
  }
}

export function providerTestPayload(stage: string, provider: string, config: JsonObject): JsonObject {
  return { stage, provider, config }
}

export function commandBody(input: {
  type: CommandType
  deviceId?: string
  agentId?: string
  payload: JsonObject
}): JsonObject {
  const body: JsonObject = {
    type: input.type,
    status: 'pending',
    payload: input.payload,
  }
  if (input.deviceId !== undefined && input.deviceId !== '') body.device = input.deviceId
  if (input.agentId !== undefined && input.agentId !== '') body.agent = input.agentId
  return body
}

export function commandStatusText(command: Command): string {
  switch (command.status) {
    case 'pending':
      return 'Pending'
    case 'running':
      return 'Running'
    case 'done':
      return 'Done'
    case 'error':
      return command.error === '' ? 'Error' : `Error: ${command.error}`
  }
}

export function readCommand(input: unknown): Command {
  if (!isJsonObject(input)) throw new Error('Command is missing')
  const id = input.id
  if (typeof id !== 'string' || id.trim() === '') throw new Error('Command id is missing')
  if (!isStatus(input.status)) throw new Error('Command status is missing')
  const error = input.error
  if (error !== undefined && error !== null && error !== '' && typeof error !== 'string') {
    throw new Error('Command error must be text')
  }
  return {
    id,
    status: input.status,
    error: typeof error === 'string' ? error : '',
  }
}

function isStatus(value: unknown): value is CommandStatus {
  return typeof value === 'string' && STATUSES.some((status) => status === value)
}
