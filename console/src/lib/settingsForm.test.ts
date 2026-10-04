import { describe, expect, it } from 'vitest'
import {
  RESTART_REQUIRED,
  fieldValues,
  isRestartRequired,
  settingsValue,
  type SettingsBlock,
} from '@/lib/settingsForm'

const mqtt: SettingsBlock = {
  key: 'mqtt',
  title: 'MQTT',
  fields: [
    { path: 'broker', label: 'Broker', kind: 'text' },
    { path: 'port', label: 'Port', kind: 'number' },
    { path: 'transport_offline_grace_period', label: 'Offline grace', kind: 'text' },
    { path: 'password', label: 'Password', kind: 'text' },
  ],
}

describe('settings form', () => {
  it('marks the keys the running process reads once', () => {
    expect([...RESTART_REQUIRED]).toEqual([
      'mqtt.transport_offline_grace_period',
      'mqtt.transport_offline_grace_period_seconds',
    ])
    expect(isRestartRequired('mqtt', 'transport_offline_grace_period')).toBe(true)
    expect(isRestartRequired('mqtt', 'port')).toBe(false)
    expect(isRestartRequired('mqtt_server', 'listen_port')).toBe(false)
    expect(isRestartRequired('ota', 'test.websocket.url')).toBe(false)
  })

  it('drops secrets and writes the edited fields', () => {
    const saved = settingsValue(
      mqtt,
      { broker: 'old', password: 'secret', extra: true },
      fieldValues({ broker: 'old', password: 'secret', extra: true }, mqtt),
    )
    const edited = { ...saved }
    const next = settingsValue(mqtt, edited, {
      ...fieldValues(edited, mqtt),
      broker: '127.0.0.1',
      port: '2883',
    })
    expect(next).toEqual({ broker: '127.0.0.1', port: 2883, extra: true })
  })

  it('round-trips a nested OTA url', () => {
    const block: SettingsBlock = {
      key: 'ota',
      title: 'OTA',
      fields: [{ path: 'test.websocket.url', label: 'Test WebSocket URL', kind: 'text' }],
    }
    const original = { test: { websocket: { url: 'ws://old/' }, mqtt: { enable: false } }, signature_key: 'sig' }
    const values = fieldValues(original, block)
    values['test.websocket.url'] = 'ws://new/xiaozhi/v1/'
    expect(settingsValue(block, original, values)).toEqual({
      test: { websocket: { url: 'ws://new/xiaozhi/v1/' }, mqtt: { enable: false } },
    })
  })
})
