import { afterEach, describe, expect, it } from 'vitest'
import { parsePeriod, type Period } from '@/shared/lib'

afterEach(() => {
  process.env.TZ = 'UTC'
})

describe('parsePeriod', () => {
  it.each<[string | undefined, string | undefined, Period]>([
    [undefined, undefined, { valid: true, from: undefined, to: undefined }],
    // to is the start of the day after: the API keeps what starts before it.
    [
      '2026-09-01',
      '2026-09-30',
      { valid: true, from: '2026-09-01T00:00:00.000Z', to: '2026-10-01T00:00:00.000Z' },
    ],
    [
      '2026-09-28',
      '2026-09-28',
      { valid: true, from: '2026-09-28T00:00:00.000Z', to: '2026-09-29T00:00:00.000Z' },
    ],
    ['2026-09-28', undefined, { valid: true, from: '2026-09-28T00:00:00.000Z', to: undefined }],
    [undefined, '2026-12-31', { valid: true, from: undefined, to: '2027-01-01T00:00:00.000Z' }],
    ['2026-09-29', '2026-09-28', { valid: false, error: 'inverted' }],
    ['28/09/2026', undefined, { valid: false, error: 'invalidFrom' }],
    ['2026-02-30', undefined, { valid: false, error: 'invalidFrom' }],
    [undefined, 'soon', { valid: false, error: 'invalidTo' }],
  ])('%s to %s → %j', (from, to, want) => {
    expect(parsePeriod(from, to)).toEqual(want)
  })

  it('counts days in the time zone of the reader', () => {
    process.env.TZ = 'Europe/Stockholm'
    expect(parsePeriod('2026-09-28', '2026-10-25')).toEqual({
      valid: true,
      from: '2026-09-27T22:00:00.000Z',
      // Summer time ends on the 25th: the 26th starts at UTC+1.
      to: '2026-10-25T23:00:00.000Z',
    })
  })
})
