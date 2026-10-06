const minute = 60_000
const hour = 60 * minute
const day = 24 * hour

// formatAge tells how long ago a time was ("3 min ago", "now"), in the largest whole unit
// up to days. A time ahead of now (clock skew) is now.
export function formatAge(iso: string, now: number, locale: string): string {
  const age = Math.max(0, now - Date.parse(iso))
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto', style: 'short' })
  if (age < minute) return rtf.format(0, 'second')
  if (age < hour) return rtf.format(-Math.floor(age / minute), 'minute')
  if (age < day) return rtf.format(-Math.floor(age / hour), 'hour')
  return rtf.format(-Math.floor(age / day), 'day')
}

// formatDateTime shows a time of today as its time only, any other with its date.
export function formatDateTime(iso: string, now: number, locale: string, timeZone?: string) {
  const t = new Date(iso)
  const dayOf = (d: Date) => new Intl.DateTimeFormat('en-CA', { timeZone }).format(d)
  const today = dayOf(t) === dayOf(new Date(now))
  return new Intl.DateTimeFormat(locale, {
    timeZone,
    timeStyle: 'short',
    dateStyle: today ? undefined : 'medium',
  }).format(t)
}

// formatBucket names an interval of the statistics by its start: a day, the Monday of a
// week, or a month.
export function formatBucket(
  iso: string,
  bucket: 'day' | 'week' | 'month',
  locale: string,
  timeZone?: string,
): string {
  const options: Intl.DateTimeFormatOptions =
    bucket === 'month' ? { month: 'short', year: 'numeric' } : { day: 'numeric', month: 'short' }
  return new Intl.DateTimeFormat(locale, { timeZone, ...options }).format(new Date(iso))
}
