import { asJsonObject, isJsonObject, isJsonValue, type JsonObject, type JsonValue } from '@/lib/json'

export type CommandType =
  | 'inject_msg'
  | 'provider_test'
  | 'settings_reload'
  | 'mcp_tools'
  | 'mcp_call'
  | 'mcp_endpoint'

export type CommandStatus = 'pending' | 'running' | 'done' | 'error'

export type Command = {
  id: string
  status: CommandStatus
  error: string
  result: JsonValue | null
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

export function mcpToolsPayload(deviceMac: string): JsonObject {
  return { device: deviceMac }
}

export function mcpCallPayload(deviceMac: string, tool: string, args: JsonObject): JsonObject {
  return { device: deviceMac, tool, arguments: args }
}

export function mcpEndpointPayload(agentId: string): JsonObject {
  return { agent: agentId }
}

export type DeviceTool = {
  name: string
  description: string
  inputSchema: JsonObject | null
}

export function readToolList(result: unknown): DeviceTool[] {
  if (!isJsonObject(result) || !Array.isArray(result.tools)) throw new Error('Tool list is missing')
  return result.tools.map(readDeviceTool)
}

export function readCallResult(result: unknown): string {
  if (!isJsonObject(result) || typeof result.result !== 'string') throw new Error('Tool result is missing')
  return result.result
}

export type McpEndpoint = {
  url: string
  connected: boolean
  toolsCount: number
}

export function readMcpEndpoint(result: unknown): McpEndpoint {
  if (!isJsonObject(result)) throw new Error('MCP access point is missing')
  if (typeof result.url !== 'string' || result.url.trim() === '') {
    throw new Error('MCP access point URL is missing')
  }
  if (typeof result.connected !== 'boolean') throw new Error('MCP access point connected state is missing')
  if (typeof result.tools_count !== 'number' || !Number.isFinite(result.tools_count)) {
    throw new Error('MCP access point tool count is missing')
  }
  return { url: result.url, connected: result.connected, toolsCount: result.tools_count }
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
  const result = input.result
  if (result !== undefined && result !== null && !isJsonValue(result)) {
    throw new Error('Command result must be JSON')
  }
  return {
    id,
    status: input.status,
    error: typeof error === 'string' ? error : '',
    result: result === undefined || result === null ? null : result,
  }
}

function readDeviceTool(input: unknown): DeviceTool {
  if (!isJsonObject(input)) throw new Error('Tool is missing')
  if (typeof input.name !== 'string' || input.name.trim() === '') throw new Error('Tool name is missing')
  if (typeof input.description !== 'string') throw new Error('Tool description is missing')
  const schema = input.input_schema
  return {
    name: input.name,
    description: input.description,
    inputSchema: schema === undefined || schema === null ? null : asJsonObject(schema, 'Tool input schema'),
  }
}

function isStatus(value: unknown): value is CommandStatus {
  return typeof value === 'string' && STATUSES.some((status) => status === value)
}
