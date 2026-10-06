// The periods and splits of the statistics page. Days are YYYY-MM-DD in the reader's
// time zone, as the period filter keeps them: from and to both included.

export type Bucket = 'day' | 'week' | 'month'
export const buckets: readonly Bucket[] = ['day', 'week', 'month']

export type Shortcut = 'thisMonth' | 'lastMonth' | 'thisYear' | 'last12Months' | 'all'
export const shortcuts: readonly Shortcut[] = [
  'thisMonth',
  'lastMonth',
  'thisYear',
  'last12Months',
  'all',
]

export type Days = { from?: string; to?: string }

// The API's limit: a year by day.
export const maxBuckets = 400

function iso(y: number, m: number, d: number): string {
  // Date normalizes the month and the day (day 0 is the last of the month before).
  const t = new Date(y, m, d)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${t.getFullYear()}-${pad(t.getMonth() + 1)}-${pad(t.getDate())}`
}

// shortcutDays is the period a shortcut stands for on the day today. "All" is open at
// the start: the API starts it with the first event.
export function shortcutDays(s: Shortcut, today: Date): Days {
  const y = today.getFullYear()
  const m = today.getMonth()
  switch (s) {
    case 'thisMonth':
      return { from: iso(y, m, 1), to: iso(y, m + 1, 0) }
    case 'lastMonth':
      return { from: iso(y, m - 1, 1), to: iso(y, m, 0) }
    case 'thisYear':
      return { from: iso(y, 0, 1), to: iso(y, 11, 31) }
    case 'last12Months':
      return { from: iso(y, m - 11, 1), to: iso(y, m + 1, 0) }
    case 'all':
      return { to: iso(y, m, today.getDate()) }
  }
}

// lastDays are the n days up to today, today included.
export function lastDays(n: number, today: Date): Days {
  const y = today.getFullYear()
  const m = today.getMonth()
  const d = today.getDate()
  return { from: iso(y, m, d - n + 1), to: iso(y, m, d) }
}

// currentShortcut is the shortcut the period is, if any. A period open at both ends is
// the whole history, as a list shows it before a period is chosen.
export function currentShortcut(days: Days, today: Date): Shortcut | undefined {
  if (!days.from && !days.to) return 'all'
  return shortcuts.find((s) => {
    const d = shortcutDays(s, today)
    return d.from === days.from && d.to === days.to
  })
}

function parse(day: string): { y: number; m: number; d: number } {
  const [y = 0, m = 1, d = 1] = day.split('-').map(Number)
  return { y, m: m - 1, d }
}

// dayCount is the number of days of a period, both ends included; undefined when it is
// open at the start. An open end is today.
function dayCount(days: Days, today: Date): number | undefined {
  if (!days.from) return undefined
  const f = parse(days.from)
  const t = days.to
    ? parse(days.to)
    : { y: today.getFullYear(), m: today.getMonth(), d: today.getDate() }
  // Whole days between two dates at noon: a change of time moves it by an hour only.
  const noon = (x: { y: number; m: number; d: number }) => new Date(x.y, x.m, x.d, 12).getTime()
  return Math.round((noon(t) - noon(f)) / 86_400_000) + 1
}

// bucketCount is the number of intervals a split gives, as the API counts them: from the
// start of the day, week or month of from to the end of the one of to.
export function bucketCount(days: Days, bucket: Bucket, today: Date): number | undefined {
  const n = dayCount(days, today)
  if (n === undefined || !days.from) return undefined
  if (bucket === 'day') return n
  const f = parse(days.from)
  if (bucket === 'week') {
    const monday = (new Date(f.y, f.m, f.d).getDay() + 6) % 7
    return Math.ceil((monday + n) / 7)
  }
  const last = new Date(f.y, f.m, f.d + n - 1)
  return (last.getFullYear() - f.y) * 12 + last.getMonth() - f.m + 1
}

// allowedBuckets are the splits within the API's limit. A period open at the start is
// not known here: every split is offered, and the API tells if one is too fine.
export function allowedBuckets(days: Days, today: Date): Bucket[] {
  return buckets.filter((b) => (bucketCount(days, b, today) ?? 0) <= maxBuckets)
}

// autoBucket splits a period by its length: days up to a month, weeks up to six months,
// months beyond, and months for a period open at the start.
export function autoBucket(days: Days, today: Date): Bucket {
  const n = dayCount(days, today)
  if (n === undefined) return 'month'
  if (n <= 31) return 'day'
  if (n <= 183) return 'week'
  return 'month'
}
