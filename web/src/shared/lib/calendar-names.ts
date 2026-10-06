// Names of the days of the week and of the months come from Intl, in the language: the
// translation files do not repeat them.

// 2024-01-01 was a Monday.
const monday = Date.UTC(2024, 0, 1)

// weekdayName names a day of the week, 0 for Monday to 6 for Sunday.
export function weekdayName(i: number, locale: string, style: 'long' | 'short' = 'long') {
  return new Intl.DateTimeFormat(locale, { weekday: style, timeZone: 'UTC' }).format(
    monday + i * 86_400_000,
  )
}

// monthName names a month, 1 to 12.
export function monthName(m: number, locale: string, style: 'long' | 'short' = 'long') {
  return new Intl.DateTimeFormat(locale, { month: style, timeZone: 'UTC' }).format(
    Date.UTC(2024, m - 1, 1),
  )
}

// formatCalendarDay shows a day (YYYY-MM-DD), not an instant: the same date anywhere.
export function formatCalendarDay(day: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' }).format(
    new Date(`${day}T00:00:00Z`),
  )
}

// isCalendarDay tells a real day written YYYY-MM-DD: not 2026-02-30 nor 2026-13-01.
export function isCalendarDay(s: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(s)) return false
  const d = new Date(`${s}T00:00:00Z`)
  return !Number.isNaN(d.getTime()) && d.toISOString().startsWith(s)
}

const utcDay = (day: string) => new Date(`${day}T00:00:00Z`)

// formatCalendarMonth names the month of a day ("October 2026").
export function formatCalendarMonth(day: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, {
    month: 'long',
    year: 'numeric',
    timeZone: 'UTC',
  }).format(utcDay(day))
}

// formatCalendarRange shows two days as one range ("3–17 Oct 2026"), Intl's way.
export function formatCalendarRange(from: string, to: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' }).formatRange(
    utcDay(from),
    utcDay(to),
  )
}
