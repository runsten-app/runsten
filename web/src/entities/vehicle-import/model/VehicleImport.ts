import type { components } from '@/shared/api'

// VehicleImport is what a vehicle holds of one source's imported trips and charges.
export type VehicleImport = components['schemas']['Import']

// ImportJob is how an import of a vehicle's history stands: pending, running, or over
// with its report.
export type ImportJob = components['schemas']['ImportJob']

// AximoteVehicle is a vehicle of an Aximote account, as its token gives access to it.
export type AximoteVehicle = components['schemas']['AximoteVehicle']

// FileImport is the report of an import of files, written at once.
export type FileImport = components['schemas']['FileImport']

// isActive tells a job the import worker has still to run.
export const isActive = (job: ImportJob | null | undefined) =>
  job?.status === 'pending' || job?.status === 'running'

export const importKeys = {
  list: (vehicle: string) => ['imports', vehicle] as const,
  job: (vehicle: string) => ['imports', vehicle, 'job'] as const,
}
