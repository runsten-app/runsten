import { api, unwrap } from '@/shared/api'
import type { Bucket, Stats } from '../model/Stats'

// StatsQuery is a period (RFC 3339 instants, either open), the IANA time zone of its
// days, and its split; without bucket, the totals only.
export type StatsQuery = { from?: string; to?: string; tz: string; bucket?: Bucket }

export function fetchStats(vehicle: string, query: StatsQuery): Promise<Stats> {
  return unwrap(api.GET('/vehicles/{vehicle}/stats', { params: { path: { vehicle }, query } }))
}
