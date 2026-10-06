import { describe, expect, it } from 'vitest'
import {
  dayKey,
  formatBounds,
  formatDay,
  formatDuration,
  formatTime,
  formatTotalDuration,
  honestDuration,
  type Bounds,
} from '@/shared/lib'

// Intl writes no-break spaces in some locales: the tables compare plain ones.
const plain = (s: string) => s.replace(/\s/g, ' ')
const min = 60_000

const start: Bounds = { after: '2026-09-28T06:55:00Z', before: '2026-09-28T07:01:00Z' }
const late: Bounds = { after: '2026-09-28T23:55:00Z', before: '2026-09-29T00:05:00Z' }
const instant: Bounds = { after: '2026-09-28T07:01:00Z', before: '2026-09-28T07:01:00Z' }

describe('formatBounds', () => {
  it.each<[Bounds, string, string | undefined, string]>([
    [start, 'en-GB', '2026-09-28', '06:55–07:01'],
    [start, 'fr-FR', '2026-09-28', '06:55 – 07:01'],
    [start, 'sv-SE', '2026-09-28', '06:55–07:01'],
    // Another day, or none: the date, told once.
    [start, 'en-GB', undefined, '28 Sept 2026, 06:55–07:01'],
    [start, 'fr-FR', '2026-09-27', '28 sept. 2026, 06:55 – 07:01'],
    [start, 'sv-SE', undefined, '28 sep. 2026 06:55–07:01'],
    // Across midnight: both dates.
    [late, 'en-GB', '2026-09-28', '28 Sept 2026, 23:55 – 29 Sept 2026, 00:05'],
    [late, 'fr-FR', '2026-09-28', '28 sept. 2026, 23:55 – 29 sept. 2026, 00:05'],
    [late, 'sv-SE', '2026-09-28', '28 sep. 2026 23:55–29 sep. 2026 00:05'],
    // Bounds that coincide are one time.
    [instant, 'en-GB', '2026-09-28', '07:01'],
    [instant, 'fr-FR', undefined, '28 sept. 2026, 07:01'],
  ])('%j in %s on %s → %s', (b, locale, day, want) => {
    expect(plain(formatBounds(b, locale, day, 'UTC'))).toBe(want)
  })

  it('follows the time zone of the reader', () => {
    // 06:55 UTC is 08:55 in Stockholm, still on the 28th.
    expect(plain(formatBounds(start, 'sv-SE', '2026-09-28', 'Europe/Stockholm'))).toBe(
      '08:55–09:01',
    )
    // 23:55 UTC is already the 29th there.
    expect(dayKey(late.after, 'Europe/Stockholm')).toBe('2026-09-29')
    expect(plain(formatBounds(late, 'sv-SE', '2026-09-29', 'Europe/Stockholm'))).toBe('01:55–02:05')
  })
})

describe('formatTime', () => {
  it.each([
    ['2026-09-28T07:01:00Z', 'en-GB', '2026-09-28', '07:01'],
    ['2026-09-29T00:05:00Z', 'en-GB', '2026-09-28', '29 Sept 2026, 00:05'],
    ['2026-09-29T00:05:00Z', 'fr-FR', '2026-09-28', '29 sept. 2026, 00:05'],
    ['2026-09-29T00:05:00Z', 'sv-SE', '2026-09-28', '29 sep. 2026 00:05'],
  ])('%s in %s on %s → %s', (iso, locale, day, want) => {
    expect(plain(formatTime(iso, locale, day, 'UTC'))).toBe(want)
  })
})

describe('formatDay', () => {
  it.each([
    ['en-GB', 'Monday, 28 September 2026'],
    ['fr-FR', 'lundi 28 septembre 2026'],
    ['sv-SE', 'måndag 28 september 2026'],
  ])('in %s → %s', (locale, want) => {
    expect(formatDay('2026-09-28T06:55:00Z', locale, 'UTC')).toBe(want)
  })
})

describe('honestDuration', () => {
  it('spans from the latest start to the earliest end, and from the earliest to the latest', () => {
    const end = { after: '2026-09-28T07:39:00Z', before: '2026-09-28T07:40:00Z' }
    expect(honestDuration(start, end)).toEqual({ min: 38 * min, max: 45 * min })
  })

  it('is never negative', () => {
    const b = { after: '2026-09-28T07:00:00Z', before: '2026-09-28T07:10:00Z' }
    expect(honestDuration(b, b)).toEqual({ min: 0, max: 10 * min })
  })
})

describe('formatDuration', () => {
  it.each<[number, number, string, string]>([
    [38, 45, 'en-GB', '38–45 mins'],
    [38, 45, 'fr-FR', '38–45 min'],
    [38, 45, 'sv-SE', '38–45 min'],
    [38, 38, 'en-GB', '38 mins'],
    [38, 38, 'fr-FR', '38 min'],
    [0, 1, 'en-GB', '0–1 mins'],
    // Beyond an hour, hours and minutes: Intl.DurationFormat has no range, two are joined.
    [55, 65, 'en-GB', '55 mins – 1 hr, 5 mins'],
    [55, 65, 'fr-FR', '55 min – 1 h et 5 min'],
    [55, 65, 'sv-SE', '55 min – 1 tim, 5 min'],
    [205, 216, 'en-GB', '3 hrs, 25 mins – 3 hrs, 36 mins'],
    [205, 216, 'fr-FR', '3 h et 25 min – 3 h et 36 min'],
    [205, 216, 'sv-SE', '3 tim, 25 min – 3 tim, 36 min'],
    [60, 60, 'en-GB', '1 hr'],
    [120, 120, 'fr-FR', '2 h'],
  ])('%s to %s min in %s → %s', (from, to, locale, want) => {
    expect(plain(formatDuration({ min: from * min, max: to * min }, locale))).toBe(want)
  })

  it('rounds outwards to whole minutes', () => {
    expect(plain(formatDuration({ min: 38.9 * min, max: 44.1 * min }, 'en-GB'))).toBe('38–45 mins')
  })
})

describe('formatTotalDuration', () => {
  const hour = 60 * min
  it.each<[number, number, string, string]>([
    // Up to ten hours, as a single event.
    [38 * min, 45 * min, 'en-GB', '38–45 mins'],
    [9 * hour, 9 * hour + 30 * min, 'en-GB', '9 hrs – 9 hrs, 30 mins'],
    // Beyond, whole hours: the minimum down, the maximum up.
    [288 * hour + 20 * min, 299 * hour + 10 * min, 'en-GB', '288–300 hrs'],
    [288 * hour + 20 * min, 299 * hour + 10 * min, 'fr-FR', '288–300 h'],
    [12 * hour, 12 * hour, 'en-GB', '12 hrs'],
  ])('%d to %d ms in %s: %s', (lo, hi, locale, want) => {
    expect(plain(formatTotalDuration({ min: lo, max: hi }, locale))).toBe(want)
  })
})
