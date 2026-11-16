import { api, unwrap } from '@/shared/api'
import type { Bucket, Parked, Stats } from '../model/Stats'

// StatsQuery is a period (RFC 3339 instants, either open), the IANA time zone of its
// days, and its split; without bucket, the totals only. place counts the charges of one
// group of charges_by_place only.
export type StatsQuery = { from?: string; to?: string; tz: string; bucket?: Bucket; place?: string }

export function fetchStats(vehicle: string, query: StatsQuery): Promise<Stats> {
  return unwrap(api.GET('/vehicles/{vehicle}/stats', { params: { path: { vehicle }, query } }))
}

// fetchParked lists the stops of a period (RFC 3339 instants, either open), newest first:
// the parked intervals the statistics sum.
export function fetchParked(
  vehicle: string,
  query: { from?: string; to?: string },
): Promise<Parked> {
  return unwrap(api.GET('/vehicles/{vehicle}/parked', { params: { path: { vehicle }, query } }))
}
