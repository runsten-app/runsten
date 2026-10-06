import { describe, expect, it } from 'vitest'
import { addDays, browserTimeZone, dayIn, timeZones } from '@/shared/lib'

describe('time zones', () => {
  it('knows the time zone of the tests', () => {
    expect(browserTimeZone()).toBe('UTC')
  })

  it('lists the zones Intl knows, sorted, with UTC and those given', () => {
    const zones = timeZones('Mars/Olympus_Mons')
    expect(zones).toContain('Europe/Paris')
    expect(zones).toContain('UTC')
    expect(zones).toContain('Mars/Olympus_Mons')
    expect(zones).toEqual([...zones].sort())
    expect(new Set(zones).size).toBe(zones.length)
  })

  it('tells the day of an instant in a zone', () => {
    const at = new Date('2026-09-27T22:30:00Z')
    expect(dayIn(at)).toBe('2026-09-27')
    expect(dayIn(at, 'Europe/Paris')).toBe('2026-09-28')
    expect(dayIn(at, 'America/New_York')).toBe('2026-09-27')
  })

  it.each([
    ['2026-09-27', 1, '2026-09-28'],
    ['2026-02-28', 1, '2026-03-01'],
    ['2028-02-28', 1, '2028-02-29'],
    ['2026-12-31', 1, '2027-01-01'],
    ['2026-03-01', -1, '2026-02-28'],
    // Calendar days, not 24 hours: the change to summer time does not matter.
    ['2026-03-29', 1, '2026-03-30'],
  ])('moves %s by %i day', (day, n, want) => expect(addDays(day, n)).toBe(want))
})
