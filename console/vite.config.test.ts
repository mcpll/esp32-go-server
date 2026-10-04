import { describe, expect, it } from 'vitest'
import config from './vite.config.ts'

describe('vite', () => {
  it('proxies /api to PocketBase and builds into pb_public', () => {
    expect(config.server?.proxy).toEqual({
      '/api': { target: 'http://127.0.0.1:8090', changeOrigin: true },
    })
    expect(config.build?.outDir).toBe('../pb_public')
    expect(config.build?.emptyOutDir).toBe(true)
  })
})
