import type { LocationQuery } from 'vue-router'

// PeriodQuery is the period as the URL holds it: whole days (YYYY-MM-DD), either open.
export type PeriodQuery = { from?: string; to?: string }

// periodQuery picks the period out of a route's query, to carry it to another list or to
// a details page and back.
export function periodQuery(query: LocationQuery): PeriodQuery {
  const one = (v: LocationQuery[string] | undefined) =>
    typeof v === 'string' && v !== '' ? v : undefined
  const p: PeriodQuery = {}
  const from = one(query.from)
  const to = one(query.to)
  if (from) p.from = from
  if (to) p.to = to
  return p
}
