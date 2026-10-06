import type { OrphanCost } from '@/entities/charge'

// The charge an orphaned cost was entered for was detected again with other bounds, or
// split, or merged: its candidates are the charges of its vehicle that overlap its window
// widened by this much on each side.
export const candidateMargin = 12 * 60 * 60 * 1000

// candidatePeriod is the period of an orphan's candidates, as the list of charges takes
// it (RFC 3339 instants, charges that overlap it).
export function candidatePeriod(o: OrphanCost): { from: string; to: string } {
  const at = (iso: string, by: number) => new Date(Date.parse(iso) + by).toISOString()
  return { from: at(o.window.after, -candidateMargin), to: at(o.window.before, candidateMargin) }
}
