import { describe, expect, it } from 'vitest'
import { formatCalendarDay, isCalendarDay, monthName, weekdayName } from '@/shared/lib'

describe('calendar names', () => {
  it.each([
    ['en-GB', 'Monday', 'Sunday', 'January', 'December'],
    ['fr', 'lundi', 'dimanche', 'janvier', 'décembre'],
    ['sv', 'måndag', 'söndag', 'januari', 'december'],
  ])('come from Intl in %s', (locale, monday, sunday, january, december) => {
    expect(weekdayName(0, locale)).toBe(monday)
    expect(weekdayName(6, locale)).toBe(sunday)
    expect(monthName(1, locale)).toBe(january)
    expect(monthName(12, locale)).toBe(december)
  })

  it('have a short form', () => {
    expect(weekdayName(5, 'en-GB', 'short')).toBe('Sat')
    expect(monthName(9, 'en-GB', 'short')).toBe('Sept')
  })

  it('show a day of the calendar, the same anywhere', () => {
    expect(formatCalendarDay('2026-08-01', 'en-GB')).toBe('1 Aug 2026')
    expect(formatCalendarDay('2026-08-01', 'fr')).toBe('1 août 2026')
    expect(formatCalendarDay('2026-12-31', 'sv')).toBe('31 dec. 2026')
  })

  it.each([
    ['2026-08-01', true],
    ['2028-02-29', true],
    ['2026-02-29', false],
    ['2026-13-01', false],
    ['2026-00-10', false],
    ['2026-8-1', false],
    ['', false],
  ])('tells whether %j is a day', (s, want) => expect(isCalendarDay(s)).toBe(want))
})
