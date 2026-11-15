import { api, downloadPath, unwrap } from '@/shared/api'
import type { Mark, PurposeFilter, Trip, TripMap, TripPage, TripTrack } from '../model/Trip'

export type PageQuery = {
  from?: string
  to?: string
  cursor?: string
  limit?: number
  purpose?: PurposeFilter
}

export function fetchTrips(vehicle: string, query: PageQuery = {}): Promise<TripPage> {
  return unwrap(api.GET('/vehicles/{vehicle}/trips', { params: { path: { vehicle }, query } }))
}

export function fetchTrip(vehicle: string, id: string): Promise<Trip> {
  return unwrap(api.GET('/vehicles/{vehicle}/trips/{id}', { params: { path: { vehicle, id } } }))
}

export function fetchTripTrack(vehicle: string, id: string): Promise<TripTrack> {
  return unwrap(
    api.GET('/vehicles/{vehicle}/trips/{id}/track', { params: { path: { vehicle, id } } }),
  )
}

export function fetchTripMap(
  vehicle: string,
  query: { from?: string; to?: string } = {},
): Promise<TripMap> {
  return unwrap(api.GET('/vehicles/{vehicle}/trip-map', { params: { path: { vehicle }, query } }))
}

// setTripMarks marks trips by hand with a purpose, or none; rule deletes their marks, and
// the places' rules apply again.
export async function setTripMarks(vehicle: string, trips: string[], purpose: Mark): Promise<void> {
  await unwrap(
    api.PUT('/vehicles/{vehicle}/trip-marks', {
      params: { path: { vehicle } },
      body: { trips, purpose },
    }),
  )
}

// tripsCsvPath is where the browser downloads the trips of a period as a CSV file, those
// of one purpose with it.
export function tripsCsvPath(
  vehicle: string,
  query: { from?: string; to?: string; purpose?: PurposeFilter } = {},
): string {
  return downloadPath(`/vehicles/${encodeURIComponent(vehicle)}/trips.csv`, query)
}
