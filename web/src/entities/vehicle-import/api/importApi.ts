import { api, unwrap } from '@/shared/api'
import type { AximoteVehicle, FileImport, ImportJob, VehicleImport } from '../model/VehicleImport'

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

// importVolvoDataPortal imports the files the Volvo Data Portal gave: the trips, and the
// waypoints that make their tracks, if any. Written at once: the report comes back.
export async function importVolvoDataPortal(
  vehicle: string,
  trips: File,
  waypoints?: File,
): Promise<FileImport> {
  const form = new FormData()
  form.append('trips', trips)
  if (waypoints) form.append('waypoints', waypoints)
  return unwrap(
    api.POST('/vehicles/{vehicle}/imports/volvo_data_portal', {
      params: { path: { vehicle } },
      // The generated type names the files as strings: the form is sent as it is, and
      // the browser sets its boundary.
      body: form as unknown as { trips: string; waypoints?: string },
      bodySerializer: (b) => b as unknown as FormData,
    }),
  )
}
