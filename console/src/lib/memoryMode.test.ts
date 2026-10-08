import { describe, expect, it } from 'vitest'
import type { JsonObject } from '@/lib/json'
import {
  REDIS_AVAILABLE_KEY,
  consoleSettings,
  memoryChoices,
  readMemoryMode,
  redisEnabledFromSettings,
} from '@/lib/memoryMode'

describe('memory mode', () => {
  it('reads none, short, and long, and treats a missing mode as none', () => {
    expect(readMemoryMode('none')).toBe('none')
    expect(readMemoryMode('short')).toBe('short')
    expect(readMemoryMode('long')).toBe('long')
    expect(readMemoryMode(undefined)).toBe('none')
    expect(readMemoryMode('')).toBe('none')
  })

  it('rejects a mode outside none, short, and long', () => {
    expect(() => readMemoryMode('redis')).toThrow(/memory_mode/)
  })

  it('disables short and explains why when Redis is off', () => {
    expect(memoryChoices(false)).toEqual({
      choices: [
        { mode: 'none', label: 'None', disabled: false },
        { mode: 'short', label: 'Short', disabled: true },
        { mode: 'long', label: 'Long', disabled: false },
      ],
      note: 'Short memory needs Redis. It is off, so the server treats short as none.',
    })
  })

  it('offers short when Redis is on', () => {
    const offered = memoryChoices(true)
    expect(offered.note).toBeNull()
    expect(offered.choices.find((choice) => choice.mode === 'short')?.disabled).toBe(false)
  })

  it('reads the server Redis switch and hides that record from settings', () => {
    const settings: { key: string; value: JsonObject }[] = [
      { key: 'udp', value: { listen_port: 8990 } },
      { key: REDIS_AVAILABLE_KEY, value: { enabled: true } },
    ]
    expect(redisEnabledFromSettings(settings)).toBe(true)
    expect(redisEnabledFromSettings([{ key: 'udp', value: {} }])).toBe(false)
    expect(consoleSettings(settings).map((setting) => setting.key)).toEqual(['udp'])
  })

  it('rejects a Redis switch that is not true or false', () => {
    expect(() =>
      redisEnabledFromSettings([{ key: REDIS_AVAILABLE_KEY, value: { enabled: 'yes' } }]),
    ).toThrow(/redis_available/)
  })
})
