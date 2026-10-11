import { api, unwrap } from '@/shared/api'
import type { AximoteVehicle, ImportJob, VehicleImport } from '../model/VehicleImport'

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

// fetchImportJob reads the vehicle's latest import; null when it has none.
export async function fetchImportJob(vehicle: string): Promise<ImportJob | null> {
  return (
    await unwrap(api.GET('/vehicles/{vehicle}/import-job', { params: { path: { vehicle } } }))
  ).job
}

// fetchAximoteVehicles asks Aximote, with the token, for its account's vehicles. The
// token goes in the body, never in the URL.
export async function fetchAximoteVehicles(
  vehicle: string,
  token: string,
): Promise<AximoteVehicle[]> {
  return (
    await unwrap(
      api.POST('/vehicles/{vehicle}/aximote-vehicles', {
        params: { path: { vehicle } },
        body: { token },
      }),
    )
  ).items
}

// startAximoteImport imports the history of an Aximote vehicle into the vehicle, in the
// background.
export async function startAximoteImport(
  vehicle: string,
  token: string,
  aximoteVehicle: string,
): Promise<ImportJob> {
  return unwrap(
    api.POST('/vehicles/{vehicle}/import-job', {
      params: { path: { vehicle } },
      body: { source: 'aximote', token, vehicle: aximoteVehicle },
    }),
  )
}
