import { api, unwrap } from '@/shared/api'
import type { Series } from '../model/Series'

// SeriesQuery is a window of at most 7 days (RFC 3339 instants, both included); without
// to, until now.
export type SeriesQuery = { from: string; to?: string }

export function fetchSeries(vehicle: string, query: SeriesQuery): Promise<Series> {
  return unwrap(api.GET('/vehicles/{vehicle}/series', { params: { path: { vehicle }, query } }))
}
