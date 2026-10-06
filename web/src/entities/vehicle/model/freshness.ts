import type { Connection } from './Vehicle'

// A reading is stale when its latest check is older than this. The collector reads the
// rare endpoints (location, odometer) hourly: two hours is one missed pass and a margin.
export const staleAfter = 2 * 60 * 60_000

// Vehicle details (the battery capacity) are read once a day: with the same margin.
export const detailsStaleAfter = 26 * 60 * 60_000

// isStale tells whether a reading can no longer be taken as current: too old, or its
// connection lost (nothing will be read until the user connects again).
export function isStale(
  checkedAt: string,
  now: number,
  connection: Pick<Connection, 'status'>,
  after = staleAfter,
): boolean {
  return connection.status === 'reauth_required' || now - Date.parse(checkedAt) > after
}
