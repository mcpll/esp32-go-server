import { describe, expect, it } from 'vitest'
import { readAgent, readDevice } from '@/lib/records'

describe('record parsing', () => {
  it('reads an agent and strips API keys from provider config', () => {
    expect(
      readAgent({
        id: 'a1',
        name: 'Italiano',
        prompt: 'Rispondi in italiano.',
        memory_mode: 'none',
        asr_provider: 'aliyun_qwen3',
        asr_config: { language: 'it', auto_end: false, api_key: 'secret' },
        llm_provider: 'aliyun',
        llm_config: { model_name: 'qwen3.8-flash' },
        tts_provider: 'aliyun_qwen',
        tts_config: { voice: 'Cherry' },
      }),
    ).toEqual({
      id: 'a1',
      name: 'Italiano',
      prompt: 'Rispondi in italiano.',
      memoryMode: 'none',
      asrProvider: 'aliyun_qwen3',
      asrConfig: { language: 'it', auto_end: false },
      llmProvider: 'aliyun',
      llmConfig: { model_name: 'qwen3.8-flash' },
      ttsProvider: 'aliyun_qwen',
      ttsConfig: { voice: 'Cherry' },
    })
  })

  it('rejects an agent with no prompt', () => {
    expect(() =>
      readAgent({
        id: 'a1',
        name: 'Italiano',
        prompt: '',
        memory_mode: 'none',
        asr_provider: 'aliyun_qwen3',
        asr_config: {},
        llm_provider: 'aliyun',
        llm_config: {},
        tts_provider: 'aliyun_qwen',
        tts_config: {},
      }),
    ).toThrow(/prompt is missing/)
  })

  it('reads a device and its expanded agent name', () => {
    expect(
      readDevice({
        id: 'd1',
        code: '123456',
        note: 'kitchen',
        activated: false,
        online: true,
        device_id: 'AA:BB:CC:DD:EE:01',
        agent: 'a1',
        expand: { agent: { name: 'Italiano' } },
      }),
    ).toEqual({
      id: 'd1',
      code: '123456',
      note: 'kitchen',
      activated: false,
      online: true,
      deviceId: 'AA:BB:CC:DD:EE:01',
      agentId: 'a1',
      agentName: 'Italiano',
    })
  })
})
