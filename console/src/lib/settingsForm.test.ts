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
    expect(next).toEqual({ broker: '127.0.0.1', port: 2883, password: 'secret', extra: true })
  })

  it('drops the embedded broker password and keeps tls paths', () => {
    const broker: SettingsBlock = {
      key: 'mqtt_server',
      title: 'MQTT broker',
      fields: [
        { path: 'listen_port', label: 'Listen port', kind: 'number' },
        { path: 'password', label: 'Password', kind: 'text' },
        { path: 'tls.port', label: 'TLS port', kind: 'number' },
        { path: 'tls.pem', label: 'TLS certificate', kind: 'text' },
        { path: 'tls.key', label: 'TLS private key', kind: 'text' },
      ],
    }
    const original = { password: 'nope', listen_port: 1, tls: { enable: false } }
    const values = fieldValues(original, broker)
    values.listen_port = '9'
    values['tls.port'] = '8883'
    values['tls.pem'] = 'config/server.pem'
    values['tls.key'] = 'config/server.key'
    expect(settingsValue(broker, original, values)).toEqual({
      listen_port: 9,
      tls: { enable: false, port: 8883, pem: 'config/server.pem', key: 'config/server.key' },
    })
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
