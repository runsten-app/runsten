import { api, downloadPath, unwrap } from '@/shared/api'
import type { Charge, ChargePage } from '../model/Charge'

export type PageQuery = { from?: string; to?: string; cursor?: string }

export function fetchCharges(vehicle: string, query: PageQuery = {}): Promise<ChargePage> {
  return unwrap(api.GET('/vehicles/{vehicle}/charges', { params: { path: { vehicle }, query } }))
}

export function fetchCharge(vehicle: string, id: string): Promise<Charge> {
  return unwrap(api.GET('/vehicles/{vehicle}/charges/{id}', { params: { path: { vehicle, id } } }))
}

// chargesCsvPath is where the browser downloads the charges of a period as a CSV file.
export function chargesCsvPath(
  vehicle: string,
  query: { from?: string; to?: string } = {},
): string {
  return downloadPath(`/vehicles/${encodeURIComponent(vehicle)}/charges.csv`, query)
}
