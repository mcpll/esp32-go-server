import { isJsonObject } from '@/lib/json'

export type OpenClawConfig = {
  allowed: boolean
  enterKeywords: string[]
  exitKeywords: string[]
}

export type OpenClawDraft = {
  allowed: boolean
  enterPhrases: string
  exitPhrases: string
}

export type OpenClawUpdate = {
  allowed: boolean
  enter_keywords: string[]
  exit_keywords: string[]
}

export function readOpenClaw(value: unknown): OpenClawConfig {
  if (!isJsonObject(value)) throw new Error('OpenClaw block is missing')
  if (typeof value.allowed !== 'boolean') throw new Error('OpenClaw allowed must be true or false')
  return {
    allowed: value.allowed,
    enterKeywords: keywordList(value.enter_keywords, 'enter_keywords'),
    exitKeywords: keywordList(value.exit_keywords, 'exit_keywords'),
  }
}

export function draftFromOpenClaw(block: OpenClawConfig): OpenClawDraft {
  return {
    allowed: block.allowed,
    enterPhrases: block.enterKeywords.join('\n'),
    exitPhrases: block.exitKeywords.join('\n'),
  }
}

export function openClawUpdate(draft: OpenClawDraft): OpenClawUpdate {
  return {
    allowed: draft.allowed,
    enter_keywords: phraseLines(draft.enterPhrases),
    exit_keywords: phraseLines(draft.exitPhrases),
  }
}

function keywordList(value: unknown, key: string): string[] {
  if (!Array.isArray(value)) throw new Error(`OpenClaw ${key} is missing`)
  return value.map((item) => {
    if (typeof item !== 'string') throw new Error(`OpenClaw ${key} must be text`)
    return item
  })
}

function phraseLines(text: string): string[] {
  return text
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line !== '')
}
