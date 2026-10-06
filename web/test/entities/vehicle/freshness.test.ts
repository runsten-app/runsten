import { describe, expect, it } from 'vitest'
import { detailsStaleAfter, isStale, staleAfter } from '@/entities/vehicle'

const now = Date.parse('2026-09-28T19:03:00Z')
const active = { status: 'active' as const }
const lost = { status: 'reauth_required' as const }

describe('isStale', () => {
  it.each([
    ['2026-09-28T19:03:00Z', active, staleAfter, false],
    ['2026-09-28T17:03:00Z', active, staleAfter, false],
    ['2026-09-28T17:02:59Z', active, staleAfter, true],
    ['2026-09-28T19:03:00Z', lost, staleAfter, true],
    ['2026-09-28T05:00:00Z', active, staleAfter, true],
    ['2026-09-28T05:00:00Z', active, detailsStaleAfter, false],
    ['2026-09-27T17:02:59Z', active, detailsStaleAfter, true],
    ['2026-09-28T05:00:00Z', lost, detailsStaleAfter, true],
  ])('checked %s, %o, after %d ms → %s', (checkedAt, connection, after, want) => {
    expect(isStale(checkedAt, now, connection, after)).toBe(want)
  })

  it('takes two hours by default', () => {
    expect(staleAfter).toBe(2 * 60 * 60_000)
    expect(isStale('2026-09-28T17:02:59Z', now, active)).toBe(true)
  })
})
