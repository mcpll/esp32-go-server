import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const banned = ['dashscope', 'aliyuncs', 'api.openai.com', 'siliconflow']

function files(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) return files(path)
    return path.endsWith('.ts') || path.endsWith('.tsx') ? [path] : []
  })
}

describe('console sources', () => {
  it('does not call a vendor', () => {
    const hits = files(join(import.meta.dirname, 'src')).flatMap((path) => {
      const text = readFileSync(path, 'utf8')
      return banned.filter((name) => text.includes(name)).map((name) => `${path} contains ${name}`)
    })
    expect(hits).toEqual([])
  })
})
