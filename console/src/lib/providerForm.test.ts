import { describe, expect, it } from 'vitest'
import {
  agentUpdate,
  configFromDraft,
  draftFromConfig,
  withProvider,
} from '@/lib/providerForm'

const tts = {
  model: 'qwen3-tts-flash',
  voice: 'Cherry',
  language_type: 'Italian',
  stream: true,
  api_key: 'tts-secret',
}

describe('provider drafts', () => {
  it('keeps known TTS fields and extra JSON, and drops API keys', () => {
    const draft = draftFromConfig('tts', 'aliyun_qwen', tts)
    expect(draft.kind).toBe('known')
    if (draft.kind !== 'known') return
    expect(draft.values.voice).toBe('Cherry')
    expect(draft.values.language_type).toBe('Italian')
    expect(JSON.parse(draft.restText)).toEqual({ stream: true })
    expect(configFromDraft('tts', draft)).toEqual({
      model: 'qwen3-tts-flash',
      voice: 'Cherry',
      language_type: 'Italian',
      stream: true,
    })
  })

  it('round-trips the seeded LLM config, including thinking mode', () => {
    const input = {
      type: 'openai',
      model_name: 'qwen3.8-flash',
      base_url: 'https://example.test/v1',
      max_tokens: 300,
      thinking: { mode: 'disabled' },
    }
    expect(configFromDraft('llm', draftFromConfig('llm', 'aliyun', input))).toEqual(input)
  })

  it('uses raw JSON for an unknown provider', () => {
    const draft = draftFromConfig('asr', 'funasr', { host: '127.0.0.1', api_key: 'secret' })
    expect(draft).toMatchObject({ kind: 'raw', provider: 'funasr' })
    expect(configFromDraft('asr', draft)).toEqual({ host: '127.0.0.1' })
  })

  it('refuses an API key typed into the JSON', () => {
    const draft = draftFromConfig('asr', 'funasr', { host: '127.0.0.1' })
    if (draft.kind !== 'raw') throw new Error('expected raw draft')
    expect(() => configFromDraft('asr', { ...draft, restText: '{"api_key":"sk"}' })).toThrow(
      /API keys stay in the server config/,
    )
  })

  it('requires the voice when the TTS provider is known', () => {
    const draft = draftFromConfig('tts', 'aliyun_qwen', tts)
    if (draft.kind !== 'known') throw new Error('expected known draft')
    expect(() =>
      configFromDraft('tts', { ...draft, values: { ...draft.values, voice: '' } }),
    ).toThrow(/Voice is required/)
  })

  it('keeps the config when switching a known provider to other', () => {
    const known = draftFromConfig('tts', 'aliyun_qwen', tts)
    const raw = withProvider('tts', known, '')
    expect(raw.kind).toBe('raw')
    expect(configFromDraft('tts', raw)).toEqual({
      model: 'qwen3-tts-flash',
      voice: 'Cherry',
      language_type: 'Italian',
      stream: true,
    })
  })
})

describe('agent update', () => {
  it('saves the name, prompt, and the three provider blocks', () => {
    const update = agentUpdate({
      name: ' Italiano ',
      prompt: ' Rispondi in italiano. ',
      asr: draftFromConfig('asr', 'aliyun_qwen3', { language: 'it', auto_end: false }),
      llm: draftFromConfig('llm', 'aliyun', {
        type: 'openai',
        model_name: 'qwen3.8-flash',
        base_url: 'https://example.test/v1',
        max_tokens: 300,
        thinking: { mode: 'disabled' },
      }),
      tts: draftFromConfig('tts', 'aliyun_qwen', {
        model: 'qwen3-tts-flash',
        voice: 'Cherry',
        language_type: 'Italian',
      }),
    })
    expect(update).toEqual({
      name: 'Italiano',
      prompt: 'Rispondi in italiano.',
      asr_provider: 'aliyun_qwen3',
      asr_config: { language: 'it', auto_end: false },
      llm_provider: 'aliyun',
      llm_config: {
        type: 'openai',
        model_name: 'qwen3.8-flash',
        base_url: 'https://example.test/v1',
        max_tokens: 300,
        thinking: { mode: 'disabled' },
      },
      tts_provider: 'aliyun_qwen',
      tts_config: {
        model: 'qwen3-tts-flash',
        voice: 'Cherry',
        language_type: 'Italian',
      },
    })
  })
})
