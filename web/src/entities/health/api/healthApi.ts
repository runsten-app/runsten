import { api, unwrap } from '@/shared/api'
import type { Health, TyreSeries } from '../model/Health'

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
