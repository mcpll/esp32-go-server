import { describe, expect, it } from 'vitest'
import {
  commandBody,
  commandStatusText,
  injectPayload,
  mcpCallPayload,
  mcpEndpointPayload,
  mcpToolsPayload,
  providerTestPayload,
  readCallResult,
  readCommand,
  readMcpEndpoint,
  readToolList,
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

  it('asks the device for its tools', () => {
    expect(
      commandBody({
        type: 'mcp_tools',
        deviceId: 'd1',
        payload: mcpToolsPayload('AA:BB:CC:DD:EE:01'),
      }),
    ).toEqual({
      type: 'mcp_tools',
      device: 'd1',
      status: 'pending',
      payload: { device: 'AA:BB:CC:DD:EE:01' },
    })
  })

  it('calls one tool with the arguments typed in the console', () => {
    expect(
      commandBody({
        type: 'mcp_call',
        deviceId: 'd1',
        payload: mcpCallPayload('AA:BB:CC:DD:EE:01', 'set_volume', { volume: 40 }),
      }),
    ).toEqual({
      type: 'mcp_call',
      device: 'd1',
      status: 'pending',
      payload: {
        device: 'AA:BB:CC:DD:EE:01',
        tool: 'set_volume',
        arguments: { volume: 40 },
      },
    })
  })

  it('asks for the agent access point without putting the token in the console', () => {
    expect(
      commandBody({
        type: 'mcp_endpoint',
        agentId: 'k7m2n9p4q1r8s3t',
        payload: mcpEndpointPayload('k7m2n9p4q1r8s3t'),
      }),
    ).toEqual({
      type: 'mcp_endpoint',
      agent: 'k7m2n9p4q1r8s3t',
      status: 'pending',
      payload: { agent: 'k7m2n9p4q1r8s3t' },
    })
  })

  it('reads the tool list, a call result, and the access point', () => {
    const tools = readToolList({
      tools: [
        {
          name: 'set_volume',
          description: 'Set the speaker volume',
          input_schema: { type: 'object', properties: { volume: { type: 'number' } } },
        },
      ],
    })
    expect(tools).toEqual([
      {
        name: 'set_volume',
        description: 'Set the speaker volume',
        inputSchema: { type: 'object', properties: { volume: { type: 'number' } } },
      },
    ])
    expect(readCallResult({ result: 'volume set to 40' })).toBe('volume set to 40')
    expect(
      readMcpEndpoint({
        url: 'wss://eye.example/mcp?token=signed',
        connected: true,
        tools_count: 1,
      }),
    ).toEqual({
      url: 'wss://eye.example/mcp?token=signed',
      connected: true,
      toolsCount: 1,
    })
  })
})
