import { describe, expect, it } from 'vitest'
import { formatAge, formatBucket, formatDateTime } from '@/shared/lib'

const plain = (s: string) => s.replace(/\s/g, ' ')

const now = Date.parse('2026-09-28T19:03:30Z')

describe('formatAge', () => {
  it.each([
    ['2026-09-28T19:03:30Z', 'en-GB', 'now'],
    ['2026-09-28T19:03:00Z', 'en-GB', 'now'],
    ['2026-09-28T19:05:00Z', 'en-GB', 'now'],
    ['2026-09-28T19:00:00Z', 'en-GB', '3 min ago'],
    ['2026-09-28T19:00:00Z', 'fr-FR', 'il y a 3 min'],
    ['2026-09-28T19:00:00Z', 'sv-SE', 'för 3 min sen'],
    ['2026-09-28T17:00:00Z', 'en-GB', '2 hr ago'],
    ['2026-09-28T17:00:00Z', 'fr-FR', 'il y a 2 h'],
    ['2026-09-28T17:00:00Z', 'sv-SE', 'för 2 tim sedan'],
    ['2026-09-27T12:00:00Z', 'en-GB', 'yesterday'],
    ['2026-09-27T12:00:00Z', 'fr-FR', 'hier'],
    ['2026-09-27T12:00:00Z', 'sv-SE', 'i går'],
    ['2026-09-25T12:00:00Z', 'en-GB', '3 days ago'],
  ])('%s in %s → %s', (iso, locale, want) => {
    expect(plain(formatAge(iso, now, locale))).toBe(want)
  })
})

describe('formatDateTime', () => {
  it.each([
    ['2026-09-28T17:26:00Z', 'en-GB', '17:26'],
    ['2026-09-28T17:26:00Z', 'fr-FR', '17:26'],
    ['2026-09-28T17:26:00Z', 'sv-SE', '17:26'],
    ['2026-09-28T05:00:00Z', 'en-US', '5:00 AM'],
    ['2026-09-27T05:00:00Z', 'en-GB', '27 Sept 2026, 05:00'],
    ['2026-09-27T05:00:00Z', 'fr-FR', '27 sept. 2026, 05:00'],
    ['2026-09-27T05:00:00Z', 'sv-SE', '27 sep. 2026 05:00'],
  ])('%s in %s → %s (today shows the time only)', (iso, locale, want) => {
    expect(plain(formatDateTime(iso, now, locale, 'UTC'))).toBe(want)
  })

  it('tells the day in the time zone of the reader', () => {
    // 23:30 UTC on the 28th is already the 29th in Stockholm, the day after "now" there.
    expect(plain(formatDateTime('2026-09-28T23:30:00Z', now, 'sv-SE', 'Europe/Stockholm'))).toBe(
      '29 sep. 2026 01:30',
    )
  })
})

describe('formatBucket', () => {
  it.each<['day' | 'week' | 'month', string, string, string]>([
    ['day', '2026-09-28T00:00:00Z', 'en-GB', '28 Sept'],
    ['week', '2026-09-28T00:00:00Z', 'fr-FR', '28 sept.'],
    ['month', '2026-09-01T00:00:00Z', 'en-GB', 'Sept 2026'],
    ['month', '2026-09-01T00:00:00Z', 'sv-SE', 'sep. 2026'],
  ])('%s %s in %s: %s', (bucket, iso, locale, want) => {
    expect(plain(formatBucket(iso, bucket, locale))).toBe(want)
  })

  it('names the interval in the time zone it was split in', () => {
    // Midnight in Paris, 22:00 the day before in UTC.
    expect(formatBucket('2026-09-27T22:00:00Z', 'day', 'en-GB', 'Europe/Paris')).toBe('28 Sept')
  })
})
