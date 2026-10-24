import { describe, expect, it } from 'vitest'
import { poolStatsAfterEvent, readPoolStats } from '@/lib/poolStats'

describe('status screen pool stats', () => {
  it('shows the numbers from the pool_stats record', () => {
    expect(
      readPoolStats({
        id: 'ps1',
        key: 'main',
        data: {
          'asr:cafebabe': {
            total_resources: 4,
            available_resources: 3,
            in_use_resources: 1,
          },
        },
      }),
    ).toEqual({
      id: 'ps1',
      pools: [{ key: 'asr:cafebabe', kind: 'asr', inUse: 1, available: 3, total: 4 }],
    })
  })

  it('replaces the numbers when a newer record arrives', () => {
    const current = readPoolStats({
      id: 'ps1',
      key: 'main',
      data: {
        'asr:cafebabe': {
          total_resources: 4,
          available_resources: 3,
          in_use_resources: 1,
        },
      },
    })
    expect(
      poolStatsAfterEvent(current, {
        action: 'update',
        record: {
          id: 'ps1',
          key: 'main',
          data: {
            'asr:cafebabe': {
              total_resources: 4,
              available_resources: 1,
              in_use_resources: 3,
            },
          },
        },
      }),
    ).toEqual({
      id: 'ps1',
      pools: [{ key: 'asr:cafebabe', kind: 'asr', inUse: 3, available: 1, total: 4 }],
    })
  })
})
