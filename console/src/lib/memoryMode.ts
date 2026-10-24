import type { JsonObject } from '@/lib/json'

export const REDIS_AVAILABLE_KEY = 'redis_available'

export const SHORT_MEMORY_NOTE =
  'Short memory needs Redis. It is off, so the server treats short as none.'

export const MEMORY_MODES = ['none', 'short', 'long'] as const

export type MemoryMode = (typeof MEMORY_MODES)[number]

export type MemoryChoice = {
  mode: MemoryMode
  label: string
  disabled: boolean
}

const LABELS: Record<MemoryMode, string> = {
  none: 'None',
  short: 'Short',
  long: 'Long',
}

export function readMemoryMode(value: unknown): MemoryMode {
  if (value === undefined || value === null || value === '') return 'none'
  if (value === 'none' || value === 'short' || value === 'long') return value
  throw new Error('memory_mode must be none, short, or long')
}

export function memoryChoices(redisEnabled: boolean): {
  choices: MemoryChoice[]
  note: string | null
} {
  return {
    choices: MEMORY_MODES.map((mode) => ({
      mode,
      label: LABELS[mode],
      disabled: mode === 'short' && !redisEnabled,
    })),
    note: redisEnabled ? null : SHORT_MEMORY_NOTE,
  }
}

export function redisEnabledFromSettings(
  settings: readonly { key: string; value: JsonObject }[],
): boolean {
  const row = settings.find((setting) => setting.key === REDIS_AVAILABLE_KEY)
  if (row === undefined) return false
  const enabled = row.value.enabled
  if (enabled === undefined) return false
  if (typeof enabled !== 'boolean') {
    throw new Error('redis_available.enabled must be true or false')
  }
  return enabled
}

export function consoleSettings<T extends { key: string }>(settings: readonly T[]): T[] {
  return settings.filter((setting) => setting.key !== REDIS_AVAILABLE_KEY)
}
