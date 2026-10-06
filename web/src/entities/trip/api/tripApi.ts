import { api, downloadPath, unwrap } from '@/shared/api'
import type { Trip, TripPage } from '../model/Trip'

export type PageQuery = { from?: string; to?: string; cursor?: string }

export function fetchTrips(vehicle: string, query: PageQuery = {}): Promise<TripPage> {
  return unwrap(api.GET('/vehicles/{vehicle}/trips', { params: { path: { vehicle }, query } }))
}

export function fetchTrip(vehicle: string, id: string): Promise<Trip> {
  return unwrap(api.GET('/vehicles/{vehicle}/trips/{id}', { params: { path: { vehicle, id } } }))
}

// tripsCsvPath is where the browser downloads the trips of a period as a CSV file.
export function tripsCsvPath(vehicle: string, query: { from?: string; to?: string } = {}): string {
  return downloadPath(`/vehicles/${encodeURIComponent(vehicle)}/trips.csv`, query)
}
