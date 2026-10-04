import { describe, expect, it } from 'vitest'
import { activationUpdate, bindDeviceUpdate, parseDeviceCode } from '@/lib/deviceBind'

describe('device binding', () => {
  it('accepts a six-digit code', () => {
    expect(parseDeviceCode(' 123456 ')).toBe('123456')
  })

  it('rejects any other code', () => {
    expect(() => parseDeviceCode('12345')).toThrow(/six-digit/)
    expect(() => parseDeviceCode('1234567')).toThrow(/six-digit/)
    expect(() => parseDeviceCode('12a456')).toThrow(/six-digit/)
  })

  it('binds the device to an agent and marks it activated', () => {
    expect(bindDeviceUpdate('agent-1', ' kitchen ')).toEqual({
      agent: 'agent-1',
      note: 'kitchen',
      activated: true,
    })
  })

  it('refuses to activate a device that has no agent', () => {
    expect(activationUpdate(false, '')).toEqual({ activated: false })
    expect(() => activationUpdate(true, '')).toThrow(/Pick an agent/)
  })
})
