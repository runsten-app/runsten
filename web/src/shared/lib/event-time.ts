// The times of a trip or a charge are bounds: the event started (or ended) somewhere
// between after and before, and nothing finer is known.
export type Bounds = { after: string; before: string }

const minute = 60_000
const hour = 60 * minute

// dayKey is the day of a time in the reader's time zone, as YYYY-MM-DD.
export function dayKey(iso: string, timeZone?: string): string {
  return new Intl.DateTimeFormat('en-CA', { timeZone }).format(new Date(iso))
}

// formatDay names a day, as the heading of the events that started on it.
export function formatDay(iso: string, locale: string, timeZone?: string): string {
  return new Intl.DateTimeFormat(locale, { timeZone, dateStyle: 'full' }).format(new Date(iso))
}

// formatBounds shows bounds as a range ("07:01–07:05"), or a single time when they
// coincide. Bounds less than a minute apart, finer than the minutes shown, are one time
// too: their middle, to the nearest minute, not two neighbouring minutes ("07:01–07:02"
// for twenty seconds). The date is left out when both fall on day (the day of a heading),
// and told once when they fall on the same other day.
export function formatBounds(b: Bounds, locale: string, day?: string, timeZone?: string): string {
  const span = Date.parse(b.before) - Date.parse(b.after)
  if (span > 0 && span < minute) {
    const middle = Date.parse(b.after) + span / 2
    let at = new Date(Math.round(middle / minute) * minute).toISOString()
    // Rounded up past midnight, bounds of one day would show the next: down instead.
    const sameDay = dayKey(b.after, timeZone) === dayKey(b.before, timeZone)
    if (sameDay && dayKey(at, timeZone) !== dayKey(b.after, timeZone)) {
      at = new Date(Math.floor(middle / minute) * minute).toISOString()
    }
    return formatBounds({ after: at, before: at }, locale, day, timeZone)
  }
  const after = new Date(b.after)
  const before = new Date(b.before)
  const onDay =
    day !== undefined && dayKey(b.after, timeZone) === day && dayKey(b.before, timeZone) === day
  const f = new Intl.DateTimeFormat(locale, {
    timeZone,
    timeStyle: 'short',
    dateStyle: onDay ? undefined : 'medium',
  })
  return after.getTime() === before.getTime() ? f.format(after) : f.formatRange(after, before)
}

// formatTime shows a single time, with its date unless it falls on day.
export function formatTime(iso: string, locale: string, day?: string, timeZone?: string): string {
  return formatBounds({ after: iso, before: iso }, locale, day, timeZone)
}

export type Duration = { min: number; max: number }

// honestDuration is every duration the bounds allow, in ms: at least from the latest
// possible start to the earliest possible end, at most from the earliest start to the
// latest end.
export function honestDuration(start: Bounds, end: Bounds): Duration {
  const span = (from: string, to: string) => Math.max(0, Date.parse(to) - Date.parse(from))
  return { min: span(start.before, end.after), max: span(start.after, end.before) }
}

// formatDuration shows a duration in whole minutes, "38–45 min", or a single value when
// both ends round alike. Beyond an hour it counts hours and minutes ("1 hr, 5 min –
// 1 hr, 12 min"): Intl.DurationFormat writes no range, so two durations are joined.
export function formatDuration(d: Duration, locale: string): string {
  const min = Math.floor(d.min / minute)
  const max = Math.ceil(d.max / minute)
  if (max * minute < hour) {
    const f = new Intl.NumberFormat(locale, { style: 'unit', unit: 'minute', unitDisplay: 'short' })
    return min === max ? f.format(min) : f.formatRange(min, max)
  }
  const f = new Intl.DurationFormat(locale, { style: 'short' })
  const one = (m: number) => f.format({ hours: Math.floor(m / 60), minutes: m % 60 })
  return min === max ? one(min) : `${one(min)} – ${one(max)}`
}

// totalDurationBounds is a sum of durations rounded outwards as formatTotalDuration
// shows it, to the minute up to ten hours, to the hour beyond: each bound on its own,
// "at least" and "at most". Null when both round alike: one value, not a range.
export function totalDurationBounds(d: Duration): Duration | null {
  const unit = d.max < 10 * hour ? minute : hour
  const min = Math.floor(d.min / unit) * unit
  const max = Math.ceil(d.max / unit) * unit
  return min === max ? null : { min, max }
}

// formatTotalDuration shows a sum of durations: as formatDuration up to ten hours, in
// whole hours beyond ("288–300 hr"), where minutes mean nothing.
export function formatTotalDuration(d: Duration, locale: string): string {
  if (d.max < 10 * hour) return formatDuration(d, locale)
  const min = Math.floor(d.min / hour)
  const max = Math.ceil(d.max / hour)
  const f = new Intl.NumberFormat(locale, { style: 'unit', unit: 'hour', unitDisplay: 'short' })
  return min === max ? f.format(min) : f.formatRange(min, max)
}

const day = 24 * hour

// formatDays shows how long a state lasted, from hours to years: as formatDuration under
// a day, else in days ("12–13 days"), each bound to the nearest day, where an hour means
// nothing. Without a max (no earlier bound), "at least" the min, as atLeast words it.
export function formatDays(
  d: { min: number; max: number | null },
  locale: string,
  atLeast: (s: string) => string,
): string {
  if (d.max === null) return atLeast(formatDays({ min: d.min, max: d.min }, locale, atLeast))
  if (d.max < day) return formatDuration({ min: d.min, max: d.max }, locale)
  const f = new Intl.NumberFormat(locale, { style: 'unit', unit: 'day', unitDisplay: 'long' })
  const min = Math.max(1, Math.round(d.min / day))
  const max = Math.round(d.max / day)
  return min >= max ? f.format(max) : f.formatRange(min, max)
}
