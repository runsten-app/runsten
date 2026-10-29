import { api, unwrap } from '@/shared/api'
import type { Health, SoftwareVersionPage, TyreSeries } from '../model/Health'

// TyresQuery is a window of at most 366 days (RFC 3339 instants, both included); without
// to, until now.
export type TyresQuery = { from: string; to?: string }

export function fetchHealth(vehicle: string): Promise<Health> {
  return unwrap(api.GET('/vehicles/{vehicle}/health', { params: { path: { vehicle } } }))
}

export function fetchTyres(vehicle: string, query: TyresQuery): Promise<TyreSeries> {
  return unwrap(
    api.GET('/vehicles/{vehicle}/health/tyres', { params: { path: { vehicle }, query } }),
  )
}

// fetchSoftwareVersions reads a page of the versions of the vehicle's software, newest
// first: after cursor, the next_cursor of the previous page.
export function fetchSoftwareVersions(
  vehicle: string,
  cursor?: string,
): Promise<SoftwareVersionPage> {
  return unwrap(
    api.GET('/vehicles/{vehicle}/software-versions', {
      params: { path: { vehicle }, query: { cursor, limit: 10 } },
    }),
  )
}
