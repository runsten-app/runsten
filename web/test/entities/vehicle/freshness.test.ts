import { describe, expect, it } from 'vitest'
import {
  detailsStaleAfter,
  isStale,
  lastReport,
  reportLateAfter,
  staleAfter,
} from '@/entities/vehicle'

const now = Date.parse('2026-09-28T19:03:00Z')
const active = { status: 'active' as const }
const lost = { status: 'reauth_required' as const }
const notConfigured = { status: 'not_configured' as const }
const refused = { status: 'active' as const, credential: 'refused' as const }
const removed = { status: 'active' as const, credential: 'missing' as const }
const set = { status: 'active' as const, credential: 'set' as const }

describe('isStale', () => {
  it.each([
    ['2026-09-28T19:03:00Z', active, staleAfter, false],
    ['2026-09-28T17:03:00Z', active, staleAfter, false],
    ['2026-09-28T17:02:59Z', active, staleAfter, true],
    ['2026-09-28T19:03:00Z', lost, staleAfter, true],
    ['2026-09-28T19:03:00Z', notConfigured, staleAfter, true],
    ['2026-09-28T05:00:00Z', active, staleAfter, true],
    ['2026-09-28T05:00:00Z', active, detailsStaleAfter, false],
    ['2026-09-27T17:02:59Z', active, detailsStaleAfter, true],
    ['2026-09-28T05:00:00Z', lost, detailsStaleAfter, true],
    ['2026-09-28T19:03:00Z', refused, staleAfter, true],
    ['2026-09-28T19:03:00Z', removed, staleAfter, true],
    ['2026-09-28T19:03:00Z', set, staleAfter, false],
  ])('checked %s, %o, after %d ms → %s', (checkedAt, connection, after, want) => {
    expect(isStale(checkedAt, now, connection, after)).toBe(want)
  })

  it('takes two hours by default', () => {
    expect(staleAfter).toBe(2 * 60 * 60_000)
    expect(isStale('2026-09-28T17:02:59Z', now, active)).toBe(true)
  })
})

describe('lastReport', () => {
  const r = (reported_at: string | null) => ({ reported_at })
  it("takes the latest of the car's own times", () => {
    expect(
      lastReport([
        r('2026-09-28T17:00:00Z'),
        null,
        r(null),
        r('2026-09-28T18:00:00Z'),
        r('2026-09-28T16:00:00Z'),
      ]),
    ).toBe('2026-09-28T18:00:00Z')
  })

  it('is null without one', () => {
    expect(lastReport([null, r(null)])).toBeNull()
    expect(lastReport([])).toBeNull()
  })

  it('takes half an hour for late', () => {
    expect(reportLateAfter).toBe(30 * 60_000)
  })
})
