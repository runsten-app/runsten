import { api, unwrap } from '@/shared/api'
import type { VehicleImport } from '../model/VehicleImport'

export async function fetchImports(vehicle: string): Promise<VehicleImport[]> {
  return (await unwrap(api.GET('/vehicles/{vehicle}/imports', { params: { path: { vehicle } } })))
    .items
}

// deleteImport deletes every trip and charge of the vehicle imported from source.
export async function deleteImport(vehicle: string, source: string): Promise<void> {
  await unwrap(
    api.DELETE('/vehicles/{vehicle}/imports/{source}', { params: { path: { vehicle, source } } }),
  )
}
