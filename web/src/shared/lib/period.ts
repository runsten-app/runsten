// A period is a range of whole days, as a reader picks them (YYYY-MM-DD, in the reader's
// time zone), and the instants the API filters on: from the start of the first day to
// the start of the day after the last, since the API keeps the events whose start.after
// precedes to.
export type Period =
  | { valid: true; from?: string; to?: string }
  | { valid: false; error: 'invalidFrom' | 'invalidTo' | 'inverted' }

const dateRe = /^(\d{4})-(\d{2})-(\d{2})$/

// startOfDay is the first instant of a day in the reader's time zone, or null for
// anything but a real date.
function startOfDay(date: string, dayAfter = false): Date | null {
  const m = dateRe.exec(date)
  if (!m) return null
  const [y, mo, d] = [Number(m[1]), Number(m[2]) - 1, Number(m[3])]
  const t = new Date(y, mo, d)
  // 2026-02-30 would silently become March 2nd.
  if (t.getFullYear() !== y || t.getMonth() !== mo || t.getDate() !== d) return null
  return dayAfter ? new Date(y, mo, d + 1) : t
}

export function parsePeriod(from?: string, to?: string): Period {
  const start = from ? startOfDay(from) : undefined
  const end = to ? startOfDay(to, true) : undefined
  if (start === null) return { valid: false, error: 'invalidFrom' }
  if (end === null) return { valid: false, error: 'invalidTo' }
  if (start && end && start >= end) return { valid: false, error: 'inverted' }
  return { valid: true, from: start?.toISOString(), to: end?.toISOString() }
}
