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

  it('omits a cleared voice so the server default applies', () => {
    const draft = draftFromConfig('tts', 'aliyun_qwen', tts)
    if (draft.kind !== 'known') throw new Error('expected known draft')
    expect(configFromDraft('tts', { ...draft, values: { ...draft.values, voice: '' } })).toEqual({
      model: 'qwen3-tts-flash',
      language_type: 'Italian',
      stream: true,
    })
  })

  it('leaves auto_end out when the agent JSON omits it', () => {
    const input = { language: 'it' }
    expect(configFromDraft('asr', draftFromConfig('asr', 'aliyun_qwen3', input))).toEqual(input)
  })

  it('keeps auto_end false when the agent stored false', () => {
    const input = { language: 'it', auto_end: false }
    expect(configFromDraft('asr', draftFromConfig('asr', 'aliyun_qwen3', input))).toEqual(input)
  })

  it('drops api_key and keeps other credentials', () => {
    const draft = draftFromConfig('asr', 'funasr', {
      host: '127.0.0.1',
      api_key: 'k',
      access_token: 't',
      password: 'p',
      secret: 's',
    })
    expect(configFromDraft('asr', draft)).toEqual({
      host: '127.0.0.1',
      access_token: 't',
      password: 'p',
      secret: 's',
    })
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
      openclaw: {
        allowed: false,
        enterPhrases: 'apri openclaw',
        exitPhrases: 'chiudi openclaw',
      },
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
      openclaw: {
        allowed: false,
        enter_keywords: ['apri openclaw'],
        exit_keywords: ['chiudi openclaw'],
      },
    })
  })

  it('saves the OpenClaw phrases and leaves the endpoint token out', () => {
    const update = agentUpdate({
      name: 'Italiano',
      prompt: 'Rispondi in italiano.',
      asr: draftFromConfig('asr', 'aliyun_qwen3', { language: 'it' }),
      llm: draftFromConfig('llm', 'aliyun', { model_name: 'qwen3.8-flash' }),
      tts: draftFromConfig('tts', 'aliyun_qwen', { voice: 'Cherry' }),
      openclaw: {
        allowed: true,
        enterPhrases: ' apri openclaw \nentra in openclaw\n',
        exitPhrases: 'chiudi openclaw\n\nesci da openclaw',
      },
    })
    expect(update.openclaw).toEqual({
      allowed: true,
      enter_keywords: ['apri openclaw', 'entra in openclaw'],
      exit_keywords: ['chiudi openclaw', 'esci da openclaw'],
    })
    expect(update).not.toHaveProperty('token')
    expect(JSON.stringify(update.openclaw)).not.toContain('ENDPOINT_AUTH_TOKEN')
  })
})
