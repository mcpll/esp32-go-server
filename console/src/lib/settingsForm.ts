import { asJsonObject, isJsonObject, type JsonObject, type JsonValue } from '@/lib/json'

export type SettingsField = {
  path: string
  label: string
  kind: 'text' | 'number' | 'bool' | 'json'
  help?: string
}

export type SettingsBlock = {
  key: string
  title: string
  hint?: string
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
    hint: 'How this server connects to a broker.',
    fields: [
      { path: 'enable', label: 'Enabled', kind: 'bool', help: 'Connects this server to a broker. Saving reconnects, or stops the client when this is off.' },
      { path: 'broker', label: 'Broker', kind: 'text', help: 'Host of the broker this server connects to. Saving reconnects.' },
      { path: 'type', label: 'Type', kind: 'text', help: 'Connection type, usually tcp. Saving reconnects.' },
      { path: 'port', label: 'Port', kind: 'number', help: 'Broker port. Saving reconnects.' },
      { path: 'client_id', label: 'Client id', kind: 'text', help: 'Id this server uses on the broker. Saving reconnects.' },
      { path: 'username', label: 'Username', kind: 'text', help: 'Username for the broker. Saving reconnects.' },
      { path: 'password', label: 'Password', kind: 'text', help: 'Password for that broker. This is not the embedded broker secret. Saving reconnects.' },
      { path: 'transport_offline_grace_period', label: 'Offline grace', kind: 'text', help: 'How long a device stays online after the broker drops. A Go duration such as 2m. The running server reads this once, so restart it after a change.' },
      { path: 'transport_offline_grace_period_seconds', label: 'Offline grace (seconds)', kind: 'number', help: 'Same wait, in seconds, used when the duration above is empty. Restart the server after a change.' },
    ],
  },
  {
    key: 'mqtt_server',
    title: 'MQTT broker',
    hint: 'The broker embedded in this server.',
    fields: [
      { path: 'enable', label: 'Enabled', kind: 'bool', help: 'Starts the broker built into this server. Saving restarts it.' },
      { path: 'listen_host', label: 'Listen host', kind: 'text', help: 'Address the broker binds. Saving restarts it.' },
      { path: 'listen_port', label: 'Listen port', kind: 'number', help: 'Port the broker binds. Saving restarts it.' },
      { path: 'client_id', label: 'Client id', kind: 'text', help: 'Id of the broker process. Saving restarts it.' },
      { path: 'username', label: 'Username', kind: 'text', help: 'Username the broker expects. Its password stays in the server environment. Saving restarts the broker.' },
      { path: 'enable_auth', label: 'Authentication', kind: 'bool', help: 'Devices must present a signed password. The signature key stays in the environment. Saving restarts the broker.' },
      { path: 'tls.enable', label: 'TLS', kind: 'bool', help: 'Also listens with TLS. Saving restarts the broker.' },
      { path: 'tls.port', label: 'TLS port', kind: 'number', help: 'Port of the TLS listener. Saving restarts the broker.' },
      { path: 'tls.pem', label: 'TLS certificate', kind: 'text', help: 'Path of the certificate file on this machine. Saving restarts the broker.' },
      { path: 'tls.key', label: 'TLS private key', kind: 'text', help: 'Path of the private key on this machine. Saving restarts the broker.' },
    ],
  },
  {
    key: 'udp',
    title: 'UDP',
    hint: 'Audio address given to the device.',
    fields: [
      { path: 'external_host', label: 'External host', kind: 'text', help: 'Host the device is told to send audio to. The next hello uses it.' },
      { path: 'external_port', label: 'External port', kind: 'number', help: 'Port the device is told to send audio to. The next hello uses it.' },
      { path: 'listen_host', label: 'Listen host', kind: 'text', help: 'Stored with the block. The audio socket always binds 0.0.0.0.' },
      { path: 'listen_port', label: 'Listen port', kind: 'number', help: 'Port the audio socket binds. Saving rebinds it.' },
    ],
  },
  {
    key: 'ota',
    title: 'OTA',
    hint: 'Addresses the device receives when it checks in.',
    fields: [
      { path: 'test.websocket.url', label: 'Test WebSocket URL', kind: 'text', help: 'WebSocket address for a device on the local network (192.168, 10, or 127). The next check-in returns it. No restart.' },
      { path: 'test.mqtt.enable', label: 'Test MQTT', kind: 'bool', help: 'Include MQTT details in the local check-in response. The next check-in uses it.' },
      { path: 'test.mqtt.endpoint', label: 'Test MQTT endpoint', kind: 'text', help: 'Broker address given to a local device. The next check-in returns it.' },
      { path: 'external.websocket.url', label: 'External WebSocket URL', kind: 'text', help: 'WebSocket address for a device outside the local network. The next check-in returns it. No restart.' },
      { path: 'external.mqtt.enable', label: 'External MQTT', kind: 'bool', help: 'Include MQTT details for a device outside the local network. The next check-in uses it.' },
      { path: 'external.mqtt.endpoint', label: 'External MQTT endpoint', kind: 'text', help: 'Broker address given to an external device. The next check-in returns it.' },
    ],
  },
  {
    key: 'mcp',
    title: 'MCP',
    hint: 'Global MCP servers.',
    fields: [
      { path: 'global.enabled', label: 'Global MCP', kind: 'bool', help: 'Connects the shared MCP servers. Saving reloads those connections.' },
      { path: 'global.reconnect_interval', label: 'Reconnect interval (seconds)', kind: 'number', help: 'Seconds between tries after a server drops. Saving reloads the connections.' },
      { path: 'global.max_reconnect_attempts', label: 'Max reconnect attempts', kind: 'number', help: 'How many times to retry a dropped server. Saving reloads the connections.' },
      { path: 'global.servers', label: 'Servers', kind: 'json', help: 'JSON list of MCP servers. Saving reloads the connections. API keys in this JSON are ignored.' },
    ],
  },
  {
    key: 'voice_identify',
    title: 'Voiceprint',
    hint: 'Voice server that tells speakers apart.',
    fields: [
      { path: 'enable', label: 'Enabled', kind: 'bool', help: 'Asks the voice server who is speaking. The next session uses it. The agent still needs voiceprint turned on.' },
      { path: 'base_url', label: 'Base URL', kind: 'text', help: 'Address of the voice server. The next session uses it.' },
      { path: 'threshold', label: 'Threshold', kind: 'number', help: 'How close a voice must match to count. The next session uses it.' },
    ],
  },
  {
    key: 'knowledge',
    title: 'Knowledge',
    hint: 'Knowledge providers.',
    fields: [{ path: 'providers', label: 'Providers', kind: 'json', help: 'JSON of knowledge providers, such as RAGFlow. The next session uses it. API keys in this JSON are ignored.' }],
  },
  {
    key: 'vision',
    title: 'Vision',
    hint: 'Where camera frames are sent.',
    fields: [
      { path: 'enable_auth', label: 'Authentication', kind: 'bool', help: 'Requires the vision token on requests. The token stays in the server environment. The next request uses this switch.' },
      { path: 'vision_url', label: 'Vision URL', kind: 'text', help: 'Where camera frames are sent. The next request uses it.' },
    ],
  },
  {
    key: 'chat',
    title: 'Chat',
    hint: 'How long a conversation waits.',
    fields: [
      { path: 'max_idle_duration', label: 'Max idle (ms)', kind: 'number', help: 'How long a quiet session stays open, in milliseconds. Zero means it never closes for idle. The running session reads this as it goes.' },
      { path: 'chat_max_silence_duration', label: 'Silence to end a sentence (ms)', kind: 'number', help: 'Silence that ends an utterance, in milliseconds. The next session uses it.' },
      { path: 'speak_request_reuse_window_ms', label: 'Speak reuse window (ms)', kind: 'number', help: 'How long a warmed speak path can be reused, in milliseconds. The next speak uses it.' },
      { path: 'realtime_mode', label: 'Realtime mode', kind: 'number', help: 'Barge-in rule while realtime is on. 1 interrupts on voice, 2 on a recognized phrase, 3 on a known speaker, 4 stops the reply at the first text. The running session reads this as it goes.' },
    ],
  },
  {
    key: 'vad',
    title: 'VAD',
    hint: 'Voice-activity detector. The model file stays in the server config.',
    fields: [{ path: 'provider', label: 'Provider', kind: 'text', help: 'Which detector to use: webrtc_vad, silero_vad, or ten_vad. The next session uses it. The model file stays in the server config.' }],
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
    value: stripSettingsSecrets(asJsonObject(input.value, 'Setting value'), key),
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
  const out = stripSettingsSecrets(structuredClone(original), block.key)
  for (const field of block.fields) {
    if (isSecretKey(`${block.key}.${field.path}`)) continue
    writeField(out, field, values[field.path])
  }
  return stripSettingsSecrets(out, block.key)
}

function isSecretKey(path: string): boolean {
  if (path === 'mqtt.password') return false
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

export function stripSettingsSecrets(value: JsonObject, prefix = ''): JsonObject {
  const out: JsonObject = {}
  for (const [key, child] of Object.entries(value)) {
    const path = prefix ? `${prefix}.${key}` : key
    if (isSecretKey(path)) continue
    out[key] = stripChild(child, path)
  }
  return out
}

function stripChild(value: JsonValue, path: string): JsonValue {
  if (Array.isArray(value)) {
    return value.map((item) => (isJsonObject(item) ? stripSettingsSecrets(item, path) : item))
  }
  if (isJsonObject(value)) return stripSettingsSecrets(value, path)
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
