import { asJsonObject, isJsonObject, type JsonObject, type JsonValue } from '@/lib/json'

export type SettingsField = {
  path: string
  label: string
  kind: 'text' | 'number' | 'bool' | 'json'
}

export type SettingsBlock = {
  key: string
  title: string
  fields: readonly SettingsField[]
}

// Read once when the MQTT adapter is built. ReloadMqttClient replaces the
// client and leaves this value on the old adapter (app.go mqttOfflineGracePeriod).
// Listen ports are not here: ReloadMqttServer and ReloadMqttUdpWithFlags rebind them.
export const RESTART_REQUIRED = [
  'mqtt.transport_offline_grace_period',
  'mqtt.transport_offline_grace_period_seconds',
] as const

const RESTART_REQUIRED_SET = new Set<string>(RESTART_REQUIRED)

const SECRET_KEYS = new Set([
  'api_key',
  'apikey',
  'api_secret',
  'access_token',
  'token',
  'password',
  'signature_key',
])

export const SETTINGS_BLOCKS: readonly SettingsBlock[] = [
  {
    key: 'mqtt',
    title: 'MQTT',
    fields: [
      { path: 'enable', label: 'Enabled', kind: 'bool' },
      { path: 'broker', label: 'Broker', kind: 'text' },
      { path: 'type', label: 'Type', kind: 'text' },
      { path: 'port', label: 'Port', kind: 'number' },
      { path: 'client_id', label: 'Client id', kind: 'text' },
      { path: 'username', label: 'Username', kind: 'text' },
      { path: 'transport_offline_grace_period', label: 'Offline grace', kind: 'text' },
      { path: 'transport_offline_grace_period_seconds', label: 'Offline grace (seconds)', kind: 'number' },
    ],
  },
  {
    key: 'mqtt_server',
    title: 'MQTT broker',
    fields: [
      { path: 'enable', label: 'Enabled', kind: 'bool' },
      { path: 'listen_host', label: 'Listen host', kind: 'text' },
      { path: 'listen_port', label: 'Listen port', kind: 'number' },
      { path: 'client_id', label: 'Client id', kind: 'text' },
      { path: 'username', label: 'Username', kind: 'text' },
      { path: 'enable_auth', label: 'Authentication', kind: 'bool' },
      { path: 'tls.enable', label: 'TLS', kind: 'bool' },
    ],
  },
  {
    key: 'udp',
    title: 'UDP',
    fields: [
      { path: 'external_host', label: 'External host', kind: 'text' },
      { path: 'external_port', label: 'External port', kind: 'number' },
      { path: 'listen_host', label: 'Listen host', kind: 'text' },
      { path: 'listen_port', label: 'Listen port', kind: 'number' },
    ],
  },
  {
    key: 'ota',
    title: 'OTA',
    fields: [
      { path: 'test.websocket.url', label: 'Test WebSocket URL', kind: 'text' },
      { path: 'test.mqtt.enable', label: 'Test MQTT', kind: 'bool' },
      { path: 'test.mqtt.endpoint', label: 'Test MQTT endpoint', kind: 'text' },
      { path: 'external.websocket.url', label: 'External WebSocket URL', kind: 'text' },
      { path: 'external.mqtt.enable', label: 'External MQTT', kind: 'bool' },
      { path: 'external.mqtt.endpoint', label: 'External MQTT endpoint', kind: 'text' },
    ],
  },
  {
    key: 'mcp',
    title: 'MCP',
    fields: [
      { path: 'global.enabled', label: 'Global MCP', kind: 'bool' },
      { path: 'global.reconnect_interval', label: 'Reconnect interval (seconds)', kind: 'number' },
      { path: 'global.max_reconnect_attempts', label: 'Max reconnect attempts', kind: 'number' },
      { path: 'global.servers', label: 'Servers', kind: 'json' },
    ],
  },
  {
    key: 'voice_identify',
    title: 'Voiceprint',
    fields: [
      { path: 'enable', label: 'Enabled', kind: 'bool' },
      { path: 'base_url', label: 'Base URL', kind: 'text' },
      { path: 'threshold', label: 'Threshold', kind: 'number' },
    ],
  },
  {
    key: 'knowledge',
    title: 'Knowledge',
    fields: [{ path: 'providers', label: 'Providers', kind: 'json' }],
  },
  {
    key: 'vision',
    title: 'Vision',
    fields: [
      { path: 'enable_auth', label: 'Authentication', kind: 'bool' },
      { path: 'vision_url', label: 'Vision URL', kind: 'text' },
    ],
  },
  {
    key: 'chat',
    title: 'Chat',
    fields: [
      { path: 'max_idle_duration', label: 'Max idle (ms)', kind: 'number' },
      { path: 'chat_max_silence_duration', label: 'Silence to end a sentence (ms)', kind: 'number' },
      { path: 'speak_request_reuse_window_ms', label: 'Speak reuse window (ms)', kind: 'number' },
      { path: 'realtime_mode', label: 'Realtime mode', kind: 'number' },
    ],
  },
  {
    key: 'vad',
    title: 'VAD',
    fields: [{ path: 'provider', label: 'Provider', kind: 'text' }],
  },
]

export type Setting = {
  id: string
  key: string
  value: JsonObject
}

export function readSetting(input: unknown): Setting {
  if (!isJsonObject(input)) throw new Error('Setting is missing')
  const id = input.id
  const key = input.key
  if (typeof id !== 'string' || id.trim() === '') throw new Error('Setting id is missing')
  if (typeof key !== 'string' || key.trim() === '') throw new Error('Setting key is missing')
  return {
    id,
    key,
    value: stripSettingsSecrets(asJsonObject(input.value, 'Setting value')),
  }
}

export type FieldValues = { [path: string]: string | boolean | undefined }

export function blockFor(key: string): SettingsBlock | null {
  return SETTINGS_BLOCKS.find((block) => block.key === key) ?? null
}

export function isRestartRequired(blockKey: string, path: string): boolean {
  return RESTART_REQUIRED_SET.has(`${blockKey}.${path}`)
}

export function fieldValues(value: JsonObject, block: SettingsBlock): FieldValues {
  const values: FieldValues = {}
  for (const field of block.fields) {
    values[field.path] = readField(value, field)
  }
  return values
}

export function settingsValue(block: SettingsBlock, original: JsonObject, values: FieldValues): JsonObject {
  const out = stripSettingsSecrets(structuredClone(original))
  for (const field of block.fields) {
    if (isSecretPath(field.path)) continue
    writeField(out, field, values[field.path])
  }
  return stripSettingsSecrets(out)
}

function isSecretPath(path: string): boolean {
  const leaf = path.split('.').pop() ?? path
  return SECRET_KEYS.has(leaf)
}

export function rawSettingsValue(text: string): JsonObject {
  return stripSettingsSecrets(parseObject(text, 'Settings'))
}

function readField(value: JsonObject, field: SettingsField): string | boolean | undefined {
  const current = readPath(value, field.path)
  if (field.kind === 'bool') {
    if (current === undefined) return undefined
    return current === true
  }
  if (field.kind === 'json') {
    if (current === undefined) return ''
    return JSON.stringify(current, null, 2)
  }
  if (typeof current === 'number') return String(current)
  if (typeof current === 'string') return current
  return ''
}

function writeField(object: JsonObject, field: SettingsField, value: string | boolean | undefined): void {
  if (field.kind === 'bool') {
    if (typeof value !== 'boolean') {
      deletePath(object, field.path)
      return
    }
    writePath(object, field.path, value)
    return
  }
  if (typeof value !== 'string' || value.trim() === '') {
    deletePath(object, field.path)
    return
  }
  if (field.kind === 'number') {
    if (!/^\d+(\.\d+)?$/.test(value.trim())) {
      throw new Error(`${field.label} must be a number`)
    }
    writePath(object, field.path, Number(value.trim()))
    return
  }
  if (field.kind === 'json') {
    writePath(object, field.path, parseJson(value, field.label))
    return
  }
  writePath(object, field.path, value)
}

function parseObject(text: string, label: string): JsonObject {
  const parsed = parseJson(text, label)
  return asJsonObject(parsed, label)
}

function parseJson(text: string, label: string): JsonValue {
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    throw new Error(`${label} is not valid JSON`)
  }
  if (!isJsonValue(parsed)) throw new Error(`${label} is not valid JSON`)
  return parsed
}

function isJsonValue(value: unknown): value is JsonValue {
  if (value === null) return true
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') return true
  if (Array.isArray(value)) return value.every(isJsonValue)
  if (isJsonObject(value)) return Object.values(value).every(isJsonValue)
  return false
}

export function stripSettingsSecrets(value: JsonObject): JsonObject {
  const out: JsonObject = {}
  for (const [key, child] of Object.entries(value)) {
    if (SECRET_KEYS.has(key)) continue
    out[key] = stripChild(child)
  }
  return out
}

function stripChild(value: JsonValue): JsonValue {
  if (Array.isArray(value)) {
    return value.map((item) => (isJsonObject(item) ? stripSettingsSecrets(item) : item))
  }
  if (isJsonObject(value)) return stripSettingsSecrets(value)
  return value
}

function readPath(object: JsonObject, path: string): JsonValue | undefined {
  let current: JsonValue = object
  for (const part of path.split('.')) {
    if (!isJsonObject(current) || !Object.hasOwn(current, part)) return undefined
    current = current[part]
  }
  return current
}

function writePath(object: JsonObject, path: string, value: JsonValue): void {
  const parts = path.split('.')
  let cursor = object
  for (let i = 0; i < parts.length - 1; i++) {
    const part = parts[i]
    const next = cursor[part]
    const child: JsonObject = isJsonObject(next) ? { ...next } : {}
    cursor[part] = child
    cursor = child
  }
  cursor[parts[parts.length - 1]] = value
}

function deletePath(object: JsonObject, path: string): void {
  const parts = path.split('.')
  const stack: JsonObject[] = [object]
  for (let i = 0; i < parts.length - 1; i++) {
    const next = stack[i][parts[i]]
    if (!isJsonObject(next)) return
    const child: JsonObject = { ...next }
    stack[i][parts[i]] = child
    stack.push(child)
  }
  delete stack[stack.length - 1][parts[parts.length - 1]]
  for (let i = stack.length - 1; i > 0; i--) {
    if (Object.keys(stack[i]).length > 0) return
    delete stack[i - 1][parts[i - 1]]
  }
}
