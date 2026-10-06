import { describe, expect, it } from 'vitest'
import {
  allowedBuckets,
  autoBucket,
  bucketCount,
  currentShortcut,
  lastDays,
  shortcutDays,
  type Days,
  type Shortcut,
} from '@/shared/lib'

const today = new Date(2026, 8, 26) // Saturday 26 September 2026
const january = new Date(2026, 0, 15)

describe('shortcutDays', () => {
  it.each<[Shortcut, Date, Days]>([
    ['thisMonth', today, { from: '2026-09-01', to: '2026-09-30' }],
    ['lastMonth', today, { from: '2026-08-01', to: '2026-08-31' }],
    ['thisYear', today, { from: '2026-01-01', to: '2026-12-31' }],
    ['last12Months', today, { from: '2025-10-01', to: '2026-09-30' }],
    ['all', today, { to: '2026-09-26' }],
    // Across a new year, and February.
    ['lastMonth', january, { from: '2025-12-01', to: '2025-12-31' }],
    ['last12Months', january, { from: '2025-02-01', to: '2026-01-31' }],
    ['thisMonth', new Date(2028, 1, 10), { from: '2028-02-01', to: '2028-02-29' }],
  ])('%s on %s', (s, day, want) => {
    expect(shortcutDays(s, day)).toEqual(want)
  })

  it('knows the period of a shortcut, and none for another', () => {
    expect(currentShortcut({ from: '2026-08-01', to: '2026-08-31' }, today)).toBe('lastMonth')
    expect(currentShortcut({ to: '2026-09-26' }, today)).toBe('all')
    // Open at both ends: the whole history, as a list shows it before a period.
    expect(currentShortcut({}, today)).toBe('all')
    expect(currentShortcut({ from: '2026-08-02', to: '2026-08-31' }, today)).toBeUndefined()
  })
})

describe('lastDays', () => {
  it('counts today, across a month and a year', () => {
    expect(lastDays(30, today)).toEqual({ from: '2026-08-28', to: '2026-09-26' })
    expect(lastDays(30, january)).toEqual({ from: '2025-12-17', to: '2026-01-15' })
    expect(lastDays(1, today)).toEqual({ from: '2026-09-26', to: '2026-09-26' })
  })
})

describe('bucketCount', () => {
  it.each<[Days, number | undefined, number | undefined, number | undefined]>([
    [{ from: '2026-09-01', to: '2026-09-30' }, 30, 5, 1], // Tuesday 1 to Wednesday 30
    [{ from: '2026-09-28', to: '2026-10-04' }, 7, 1, 2], // Monday to Sunday
    [{ from: '2026-03-01', to: '2026-03-31' }, 31, 6, 1], // a change of time
    [{ from: '2025-10-01', to: '2026-09-30' }, 365, 53, 12],
    [{ from: '2026-09-20' }, 7, 2, 1], // until today
    [{ to: '2026-09-30' }, undefined, undefined, undefined], // open: the API decides
  ])('%j: %s days, %s weeks, %s months', (days, d, w, m) => {
    expect(bucketCount(days, 'day', today)).toBe(d)
    expect(bucketCount(days, 'week', today)).toBe(w)
    expect(bucketCount(days, 'month', today)).toBe(m)
  })

  it('offers the splits within 400 intervals', () => {
    expect(allowedBuckets({ from: '2025-01-01', to: '2026-02-04' }, today)).toEqual([
      'day',
      'week',
      'month',
    ]) // 400 days
    expect(allowedBuckets({ from: '2025-01-01', to: '2026-02-05' }, today)).toEqual([
      'week',
      'month',
    ])
    expect(allowedBuckets({ from: '1995-01-01', to: '2026-09-30' }, today)).toEqual(['month'])
    // 441 months: none, and the API will tell.
    expect(allowedBuckets({ from: '1990-01-01', to: '2026-09-30' }, today)).toEqual([])
    expect(allowedBuckets({ to: '2026-09-30' }, today)).toEqual(['day', 'week', 'month'])
  })
})

describe('autoBucket', () => {
  it.each<[Days, string]>([
    [{ from: '2026-09-01', to: '2026-10-01' }, 'day'], // 31 days
    [{ from: '2026-09-01', to: '2026-10-02' }, 'week'],
    [{ from: '2026-01-01', to: '2026-07-02' }, 'week'], // 183 days
    [{ from: '2026-01-01', to: '2026-07-03' }, 'month'],
    [{ from: '2026-09-20' }, 'day'],
    [{ to: '2026-09-30' }, 'month'],
  ])('%j → %s', (days, want) => {
    expect(autoBucket(days, today)).toBe(want)
  })
})
