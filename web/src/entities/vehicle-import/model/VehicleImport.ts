import type { components } from '@/shared/api'

// VehicleImport is what a vehicle holds of one source's imported trips and charges.
export type VehicleImport = components['schemas']['Import']

export const importKeys = {
  list: (vehicle: string) => ['imports', vehicle] as const,
}
