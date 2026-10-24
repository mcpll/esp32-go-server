import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { StatusView } from '@/components/StatusView'
import { readPoolStats } from '@/lib/poolStats'

function stats(poolKey: string, inUse: number, available: number, total: number) {
  return readPoolStats({
    id: 'ps1',
    key: 'main',
    data: {
      [poolKey]: {
        total_resources: total,
        available_resources: available,
        in_use_resources: inUse,
      },
    },
  })
}

describe('status screen', () => {
  it('shows the fresh in-use count from the latest record', () => {
    const before = renderToStaticMarkup(
      createElement(StatusView, { stats: stats('asr:cafebabe', 1, 3, 4) }),
    )
    const after = renderToStaticMarkup(
      createElement(StatusView, { stats: stats('tts:facefeed', 3, 2, 5) }),
    )
    expect(before).toContain('1 in use')
    expect(before).toContain('asr')
    expect(after).toContain('3 in use')
    expect(after).toContain('tts')
    expect(after).not.toContain('1 in use')
  })
})
