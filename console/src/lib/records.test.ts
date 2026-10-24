import { describe, expect, it } from 'vitest'
import { readAgent, readDevice } from '@/lib/records'

describe('record parsing', () => {
  it('reads an agent and strips API keys from provider config', () => {
    expect(
      readAgent({
        id: 'a1',
        name: 'Italiano',
        prompt: 'Rispondi in italiano.',
        asr_provider: 'aliyun_qwen3',
        asr_config: { language: 'it', auto_end: false, api_key: 'secret' },
        llm_provider: 'aliyun',
        llm_config: { model_name: 'qwen3.8-flash' },
        tts_provider: 'aliyun_qwen',
        tts_config: { voice: 'Cherry' },
        openclaw: {
          allowed: false,
          enter_keywords: ['apri openclaw'],
          exit_keywords: ['chiudi openclaw'],
        },
      }),
    ).toEqual({
      id: 'a1',
      name: 'Italiano',
      prompt: 'Rispondi in italiano.',
      asrProvider: 'aliyun_qwen3',
      asrConfig: { language: 'it', auto_end: false },
      llmProvider: 'aliyun',
      llmConfig: { model_name: 'qwen3.8-flash' },
      ttsProvider: 'aliyun_qwen',
      ttsConfig: { voice: 'Cherry' },
      openclaw: {
        allowed: false,
        enterKeywords: ['apri openclaw'],
        exitKeywords: ['chiudi openclaw'],
      },
    })
  })

  it('reads the seeded Italian OpenClaw phrases', () => {
    expect(
      readAgent({
        id: 'a1',
        name: 'Italiano',
        prompt: 'Rispondi in italiano.',
        asr_provider: 'aliyun_qwen3',
        asr_config: {},
        llm_provider: 'aliyun',
        llm_config: {},
        tts_provider: 'aliyun_qwen',
        tts_config: {},
        openclaw: {
          allowed: false,
          enter_keywords: ['apri openclaw', 'entra in openclaw'],
          exit_keywords: ['chiudi openclaw', 'esci da openclaw'],
        },
      }).openclaw,
    ).toEqual({
      allowed: false,
      enterKeywords: ['apri openclaw', 'entra in openclaw'],
      exitKeywords: ['chiudi openclaw', 'esci da openclaw'],
    })
  })

  it('rejects an agent with no prompt', () => {
    expect(() =>
      readAgent({
        id: 'a1',
        name: 'Italiano',
        prompt: '',
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
