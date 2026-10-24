import { describe, expect, it } from 'vitest'
import { assertNoDevicesForAgent, devicesBlockMessage } from '@/lib/deleteAgentPreflight'

describe('devicesBlockMessage', () => {
  it('describes a single bound device', () => {
    expect(devicesBlockMessage(1)).toBe(
      'One device still uses this agent. Reassign or remove it before deleting.',
    )
  })

  it('describes multiple bound devices', () => {
    expect(devicesBlockMessage(3)).toBe(
      '3 devices still use this agent. Reassign or remove them before deleting.',
    )
  })
})

describe('assertNoDevicesForAgent', () => {
  it('allows delete when no devices are bound', () => {
    expect(() => assertNoDevicesForAgent(0)).not.toThrow()
  })

  it('blocks delete when devices are bound', () => {
    expect(() => assertNoDevicesForAgent(2)).toThrow(
      '2 devices still use this agent. Reassign or remove them before deleting.',
    )
  })
})
