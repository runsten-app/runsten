import { describe, expect, it } from 'vitest'
import { expiry, renewAhead } from '@/features/manage-polestar'

const expires = '2026-12-27T05:00:00Z'
const at = Date.parse(expires)

describe('expiry', () => {
  it.each([
    [at - renewAhead - 1, 'valid'],
    [at - renewAhead, 'soon'],
    [at - 1, 'soon'],
    [at, 'past'],
    [at + 24 * 60 * 60_000, 'past'],
  ])('%d: %s', (now, want) => {
    expect(expiry(expires, now)).toBe(want)
  })

  it('reminds two weeks before', () => {
    expect(renewAhead).toBe(14 * 24 * 60 * 60_000)
  })
})
