// browserTimeZone is the reader's time zone: the server splits days, weeks and months in
// it, as the lists group their events by the reader's days; a new place starts in it.
export function browserTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone
}

// timeZones are the IANA zones the browser knows, with those given (a place's own, which
// an older browser may lack) and UTC, which Intl does not always list.
export function timeZones(...extra: string[]): string[] {
  const known = new Set(Intl.supportedValuesOf('timeZone'))
  for (const z of ['UTC', ...extra]) if (z) known.add(z)
  return [...known].sort()
}

// dayIn is the day (YYYY-MM-DD) that at falls on in a time zone: a tariff's versions
// change at midnight in their place's zone, not the reader's.
export function dayIn(at: Date, timeZone?: string): string {
  return new Intl.DateTimeFormat('en-CA', { timeZone }).format(at)
}

// addDays moves a day (YYYY-MM-DD) by n calendar days.
export function addDays(day: string, n: number): string {
  const d = new Date(`${day}T00:00:00Z`)
  d.setUTCDate(d.getUTCDate() + n)
  return d.toISOString().slice(0, 10)
}
