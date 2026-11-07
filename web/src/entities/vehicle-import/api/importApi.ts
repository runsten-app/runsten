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

// importAAOSLogging imports AAOS Logging's export: the ZIP Google Drive gives for the
// car's folder. Written at once: the report comes back.
export async function importAAOSLogging(vehicle: string, exported: File): Promise<FileImport> {
  const form = new FormData()
  form.append('export', exported)
  return unwrap(
    api.POST('/vehicles/{vehicle}/imports/aaos_logging', {
      params: { path: { vehicle } },
      // As the portal's: the form is sent as it is, and the browser sets its boundary.
      body: form as unknown as { export: string },
      bodySerializer: (b) => b as unknown as FormData,
    }),
  )
}

// isGzip tells a file gzip already compressed, by its first bytes.
async function isGzip(file: Blob): Promise<boolean> {
  const head = new Uint8Array(await file.slice(0, 2).arrayBuffer())
  return head[0] === 0x1f && head[1] === 0x8b
}

// gzipped compresses the backup before it is sent: TeslaMate's is several times smaller
// so. Without CompressionStream, or already gzipped, it goes as it is.
export async function gzipped(file: Blob): Promise<{ body: Blob; type: string }> {
  if (await isGzip(file)) return { body: file, type: 'application/gzip' }
  if (typeof CompressionStream === 'undefined')
    return { body: file, type: 'application/octet-stream' }
  const stream = file.stream().pipeThrough(new CompressionStream('gzip'))
  return { body: await new Response(stream).blob(), type: 'application/gzip' }
}

// importTeslaMate imports the history of the vehicle's car in a TeslaMate backup, its
// charges' costs in currency when given. compressed is told once the backup is ready to
// send. Written at once: the report comes back.
export async function importTeslaMate(
  vehicle: string,
  backup: File,
  currency?: string,
  compressed?: () => void,
): Promise<FileImport> {
  const { body, type } = await gzipped(backup)
  compressed?.()
  return unwrap(
    api.POST('/vehicles/{vehicle}/imports/teslamate', {
      params: { path: { vehicle }, query: currency ? { currency } : {} },
      // The generated type names the body as a string: the file is sent as it is.
      body: body as unknown as string,
      bodySerializer: (b) => b as unknown as Blob,
      headers: { 'Content-Type': type },
    }),
  )
}
