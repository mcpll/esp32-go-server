import { describe, expect, it } from 'vitest'
import { deviceRemoveConfirmMessage } from '@/lib/deleteDevicePreflight'
import type { Device } from '@/lib/records'

function sampleDevice(overrides: Partial<Device> = {}): Device {
  return {
    id: 'rec1',
    code: '123456',
    note: '',
    activated: false,
    online: false,
    deviceId: 'AA:BB:CC:DD:EE:01',
    agentId: 'agent1',
    agentName: 'Italiano',
    ...overrides,
  }
}

describe('deviceRemoveConfirmMessage', () => {
  it('uses the six-digit code when there is no note', () => {
    expect(deviceRemoveConfirmMessage(sampleDevice())).toBe(
      'Remove device 123456? This cannot be undone.',
    )
  })

  it('uses the note as the label when set', () => {
    expect(deviceRemoveConfirmMessage(sampleDevice({ note: 'Kitchen' }))).toBe(
      'Remove Kitchen? This cannot be undone.',
    )
  })

  it('warns when the device is online', () => {
    expect(deviceRemoveConfirmMessage(sampleDevice({ online: true }))).toBe(
      'Remove device 123456? This cannot be undone. The hardware may still be running; add it again with a new code.',
    )
  })

  it('warns when the device is activated', () => {
    expect(deviceRemoveConfirmMessage(sampleDevice({ activated: true }))).toBe(
      'Remove device 123456? This cannot be undone. The hardware may still be running; add it again with a new code.',
    )
  })
})
