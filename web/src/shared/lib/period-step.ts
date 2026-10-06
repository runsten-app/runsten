import type { Days } from './stats-period'

// The calendar of a period: whole months (the 1st of one to the last day of another),
// or a number of days. Days are YYYY-MM-DD in the reader's time zone, both included.

type YMD = { y: number; m: number; d: number }

function parse(day: string): YMD {
  const [y = 0, m = 1, d = 1] = day.split('-').map(Number)
  return { y, m: m - 1, d }
}

function iso(y: number, m: number, d: number): string {
  // Date normalizes the month and the day (day 0 is the last of the month before).
  const t = new Date(y, m, d)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${t.getFullYear()}-${pad(t.getMonth() + 1)}-${pad(t.getDate())}`
}

// wholeMonths is how many whole months a period spans, from the 1st of its first to the
// last day of its last; undefined for any other period, or one open at an end.
export function wholeMonths(days: Days): number | undefined {
  if (!days.from || !days.to) return undefined
  const f = parse(days.from)
  const t = parse(days.to)
  if (f.d !== 1 || iso(t.y, t.m + 1, 0) !== days.to) return undefined
  const n = (t.y - f.y) * 12 + t.m - f.m + 1
  return n > 0 ? n : undefined
}

// dayCount is the number of days of a closed period, both ends included.
function dayCount(from: string, to: string): number {
  const noon = (x: YMD) => new Date(x.y, x.m, x.d, 12).getTime()
  return Math.round((noon(parse(to)) - noon(parse(from))) / 86_400_000) + 1
}

// stepPeriod is the period just before (-1) or after (+1) a closed one, of the same
// length: a month for a month, a year for a calendar year, twelve months for twelve, else
// as many days. Undefined for a period open at an end: there is no next "everything".
export function stepPeriod(days: Days, dir: -1 | 1): Days | undefined {
  if (!days.from || !days.to) return undefined
  const months = wholeMonths(days)
  const f = parse(days.from)
  if (months !== undefined) {
    const start = f.m + dir * months
    return { from: iso(f.y, start, 1), to: iso(f.y, start + months, 0) }
  }
  const n = dayCount(days.from, days.to)
  const t = parse(days.to)
  return { from: iso(f.y, f.m, f.d + dir * n), to: iso(t.y, t.m, t.d + dir * n) }
}
