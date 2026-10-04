import { describe, expect, it } from 'vitest'
import { isReachable } from '@/lib/gate'

describe('logged-out gate', () => {
  it('allows only the login path', () => {
    expect(isReachable(false, '/login')).toBe(true)
    expect(isReachable(false, '/agents')).toBe(false)
    expect(isReachable(false, '/agents/abc')).toBe(false)
    expect(isReachable(false, '/devices')).toBe(false)
    expect(isReachable(false, '/settings')).toBe(false)
  })

  it('hides login once the owner is in', () => {
    expect(isReachable(true, '/login')).toBe(false)
    expect(isReachable(true, '/agents')).toBe(true)
    expect(isReachable(true, '/agents/abc')).toBe(true)
    expect(isReachable(true, '/devices')).toBe(true)
    expect(isReachable(true, '/settings')).toBe(true)
  })
})
