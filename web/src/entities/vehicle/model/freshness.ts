import type { Connection } from './Vehicle'

// A reading is stale when its latest check is older than this. The collector reads the
// rare endpoints (location, odometer) hourly: two hours is one missed pass and a margin.
export const staleAfter = 2 * 60 * 60_000

// Vehicle details (the battery capacity) are read once a day: with the same margin.
export const detailsStaleAfter = 26 * 60 * 60_000

// isStale tells whether a reading can no longer be taken as current: too old, or its
// connection lost (nothing will be read until the user connects again, or gives another
// Polestar credential, or the administrator sets the provider's application).
export function isStale(
  checkedAt: string,
  now: number,
  connection: Pick<Connection, 'status'> & Partial<Pick<Connection, 'credential'>>,
  after = staleAfter,
): boolean {
  return (
    connection.status === 'reauth_required' ||
    connection.status === 'not_configured' ||
    connection.credential === 'refused' ||
    connection.credential === 'missing' ||
    now - Date.parse(checkedAt) > after
  )
}

// A car uploads its readings when it chooses: a Polestar parked may give values hours old.
// Beyond this, the vehicle's page tells when the car last measured them.
export const reportLateAfter = 30 * 60_000

type Reported = { reported_at: string | null } | null

// lastReport is the car's latest own timestamp among the readings, null when none has one.
export function lastReport(readings: readonly Reported[]): string | null {
  let last: string | null = null
  for (const r of readings) {
    const at = r?.reported_at
    if (at && (!last || Date.parse(at) > Date.parse(last))) last = at
  }
  return last
}
