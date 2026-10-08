import { describe, expect, it } from 'vitest'
import {
  commandBody,
  commandStatusText,
  injectPayload,
  providerTestPayload,
  readCommand,
} from '@/lib/commands'

describe('commands', () => {
  it('builds a speak command from the text typed in the console', () => {
    expect(
      commandBody({
        type: 'inject_msg',
        deviceId: 'd1',
        payload: injectPayload('AA:BB:CC:DD:EE:01', 'ciao dalla console', true),
      }),
    ).toEqual({
      type: 'inject_msg',
      device: 'd1',
      status: 'pending',
      payload: {
        device: 'AA:BB:CC:DD:EE:01',
        message: 'ciao dalla console',
        skip_llm: true,
        auto_listen: false,
      },
    })
  })

  it('builds a chat command when the text should be answered', () => {
    expect(injectPayload('AA:BB:CC:DD:EE:01', 'che ore sono', false)).toEqual({
      device: 'AA:BB:CC:DD:EE:01',
      message: 'che ore sono',
      skip_llm: false,
      auto_listen: false,
    })
  })

  it('builds a provider test from the stage block', () => {
    expect(
      commandBody({
        type: 'provider_test',
        agentId: 'a1',
        payload: providerTestPayload('llm', 'aliyun', { model_name: 'qwen3.8-flash' }),
      }),
    ).toEqual({
      type: 'provider_test',
      agent: 'a1',
      status: 'pending',
      payload: {
        stage: 'llm',
        provider: 'aliyun',
        config: { model_name: 'qwen3.8-flash' },
      },
    })
  })

  it('shows pending, running, done and the vendor error', () => {
    expect(commandStatusText(readCommand({ id: 'c1', status: 'pending' }))).toBe('Pending')
    expect(commandStatusText(readCommand({ id: 'c1', status: 'running' }))).toBe('Running')
    expect(commandStatusText(readCommand({ id: 'c1', status: 'done', result: { ok: true } }))).toBe(
      'Done',
    )
    expect(
      commandStatusText(
        readCommand({
          id: 'c1',
          status: 'error',
          error: 'Incorrect API key provided: sk-wrong',
        }),
      ),
    ).toBe('Error: Incorrect API key provided: sk-wrong')
  })

  it('rejects a command whose status is not one of the four', () => {
    expect(() => readCommand({ id: 'c1', status: 'lost' })).toThrow(/status/)
  })
})
