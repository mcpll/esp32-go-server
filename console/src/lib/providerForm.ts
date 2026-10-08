import { asJsonObject, isJsonObject, type JsonObject, type JsonValue } from '@/lib/json'
import type { MemoryMode } from '@/lib/memoryMode'

const SECRET_KEYS = new Set(['api_key', 'apikey', 'api_secret'])

export type Stage = 'asr' | 'llm' | 'tts'

export type Field = {
  path: string
  label: string
  kind: 'text' | 'number' | 'bool'
}

const FIELDS: Record<Stage, Record<string, readonly Field[]>> = {
  asr: {
    aliyun_qwen3: [
      { path: 'language', label: 'Language', kind: 'text' },
      { path: 'auto_end', label: 'Auto end', kind: 'bool' },
    ],
  },
  llm: {
    aliyun: [
      { path: 'type', label: 'Type', kind: 'text' },
      { path: 'model_name', label: 'Model', kind: 'text' },
      { path: 'base_url', label: 'Base URL', kind: 'text' },
      { path: 'max_tokens', label: 'Max tokens', kind: 'number' },
      { path: 'thinking.mode', label: 'Thinking', kind: 'text' },
    ],
  },
  tts: {
    aliyun_qwen: [
      { path: 'model', label: 'Model', kind: 'text' },
      { path: 'voice', label: 'Voice', kind: 'text' },
      { path: 'language_type', label: 'Language', kind: 'text' },
    ],
  },
}

export function knownProviders(stage: Stage): readonly string[] {
  return Object.keys(FIELDS[stage])
}

export function fieldsFor(stage: Stage, provider: string): readonly Field[] | null {
  return FIELDS[stage][provider] ?? null
}

export function stripSecrets(value: JsonObject): JsonObject {
  const out: JsonObject = {}
  for (const [key, child] of Object.entries(value)) {
    if (SECRET_KEYS.has(key)) continue
    out[key] = stripChild(child)
  }
  return out
}

function stripChild(value: JsonValue): JsonValue {
  if (Array.isArray(value)) {
    return value.map((item) => (isJsonObject(item) ? stripSecrets(item) : item))
  }
  if (isJsonObject(value)) return stripSecrets(value)
  return value
}

export function secretKeysIn(value: JsonObject): string[] {
  const found: string[] = []
  collectSecrets(value, found)
  return found
}

function collectSecrets(value: JsonObject, found: string[]): void {
  for (const [key, child] of Object.entries(value)) {
    if (SECRET_KEYS.has(key)) found.push(key)
    if (isJsonObject(child)) collectSecrets(child, found)
    if (Array.isArray(child)) {
      for (const item of child) {
        if (isJsonObject(item)) collectSecrets(item, found)
      }
    }
  }
}

export type FieldValues = { [path: string]: string | boolean | undefined }

export type KnownDraft = {
  kind: 'known'
  provider: string
  values: FieldValues
  restText: string
}

export type RawDraft = {
  kind: 'raw'
  provider: string
  restText: string
}

export type ProviderDraft = KnownDraft | RawDraft

export function draftFromConfig(stage: Stage, provider: string, config: unknown): ProviderDraft {
  const clean = stripSecrets(asJsonObject(config, 'Provider config'))
  const fields = fieldsFor(stage, provider)
  if (fields === null) {
    return { kind: 'raw', provider, restText: JSON.stringify(clean, null, 2) }
  }
  const rest = structuredClone(clean)
  const values: FieldValues = {}
  for (const field of fields) {
    values[field.path] = takeField(rest, field)
  }
  return { kind: 'known', provider, values, restText: JSON.stringify(rest, null, 2) }
}

export function configFromDraft(stage: Stage, draft: ProviderDraft): JsonObject {
  if (draft.kind === 'raw') return parseConfigJson(draft.restText)
  const fields = fieldsFor(stage, draft.provider)
  if (fields === null) throw new Error(`Unknown provider ${draft.provider}`)
  const out = parseConfigJson(draft.restText)
  for (const field of fields) {
    writeField(out, field, draft.values[field.path])
  }
  return out
}

export function withProvider(stage: Stage, draft: ProviderDraft, provider: string): ProviderDraft {
  return draftFromConfig(stage, provider, configFromDraft(stage, draft))
}

export type AgentDraft = {
  name: string
  prompt: string
  memoryMode: MemoryMode
  asr: ProviderDraft
  llm: ProviderDraft
  tts: ProviderDraft
}

export type AgentUpdate = {
  name: string
  prompt: string
  memory_mode: MemoryMode
  asr_provider: string
  asr_config: JsonObject
  llm_provider: string
  llm_config: JsonObject
  tts_provider: string
  tts_config: JsonObject
}

export function agentUpdate(draft: AgentDraft): AgentUpdate {
  const name = draft.name.trim()
  const prompt = draft.prompt.trim()
  if (name === '') throw new Error('Name is required')
  if (prompt === '') throw new Error('Prompt is required')
  return {
    name,
    prompt,
    memory_mode: draft.memoryMode,
    asr_provider: requiredProvider(draft.asr, 'ASR'),
    asr_config: configFromDraft('asr', draft.asr),
    llm_provider: requiredProvider(draft.llm, 'LLM'),
    llm_config: configFromDraft('llm', draft.llm),
    tts_provider: requiredProvider(draft.tts, 'TTS'),
    tts_config: configFromDraft('tts', draft.tts),
  }
}

function requiredProvider(draft: ProviderDraft, label: string): string {
  const provider = draft.provider.trim()
  if (provider === '') throw new Error(`${label} provider is required`)
  return provider
}

function parseConfigJson(text: string): JsonObject {
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    throw new Error('Provider JSON is not valid')
  }
  const object = asJsonObject(parsed, 'Provider JSON')
  const secrets = secretKeysIn(object)
  if (secrets.length > 0) {
    throw new Error(`Remove ${secrets.join(', ')}. API keys stay in the server config.`)
  }
  return object
}

function takeField(rest: JsonObject, field: Field): string | boolean | undefined {
  const current = readPath(rest, field.path)
  const present = pathExists(rest, field.path)
  deletePath(rest, field.path)
  if (field.kind === 'bool') {
    if (!present) return undefined
    return current === true
  }
  if (typeof current === 'number') return String(current)
  if (typeof current === 'string') return current
  return ''
}

function writeField(object: JsonObject, field: Field, value: string | boolean | undefined): void {
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
    if (!/^\d+$/.test(value.trim())) throw new Error(`${field.label} must be a whole number`)
    writePath(object, field.path, Number(value.trim()))
    return
  }
  writePath(object, field.path, value)
}

function splitPath(path: string): [string, string | null] {
  const dot = path.indexOf('.')
  if (dot === -1) return [path, null]
  const tail = path.slice(dot + 1)
  if (tail.includes('.')) throw new Error(`Unsupported field path ${path}`)
  return [path.slice(0, dot), tail]
}

function pathExists(object: JsonObject, path: string): boolean {
  const [head, tail] = splitPath(path)
  if (!Object.hasOwn(object, head)) return false
  if (tail === null) return true
  const value = object[head]
  return isJsonObject(value) && Object.hasOwn(value, tail)
}

function readPath(object: JsonObject, path: string): JsonValue | undefined {
  const [head, tail] = splitPath(path)
  if (!Object.hasOwn(object, head)) return undefined
  const value = object[head]
  if (tail === null) return value
  if (!isJsonObject(value) || !Object.hasOwn(value, tail)) return undefined
  return value[tail]
}

function deletePath(object: JsonObject, path: string): void {
  const [head, tail] = splitPath(path)
  if (tail === null) {
    delete object[head]
    return
  }
  const existing = object[head]
  if (!isJsonObject(existing)) return
  const child: JsonObject = { ...existing }
  delete child[tail]
  if (Object.keys(child).length === 0) {
    delete object[head]
    return
  }
  object[head] = child
}

function writePath(object: JsonObject, path: string, value: JsonValue): void {
  const [head, tail] = splitPath(path)
  if (tail === null) {
    object[head] = value
    return
  }
  const existing = object[head]
  const child: JsonObject = isJsonObject(existing) ? { ...existing } : {}
  child[tail] = value
  object[head] = child
}
