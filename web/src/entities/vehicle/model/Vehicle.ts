import type { components } from '@/shared/api'

export type Vehicle = components['schemas']['Vehicle']
export type Connection = components['schemas']['Connection']
export type State = components['schemas']['State']
export type Model = components['schemas']['Model']
// VariantOption is a variant a vehicle may be, with where the catalog has its figures from.
export type VariantOption = components['schemas']['VariantOption']
export type SourceLevel = VariantOption['levels']['gross_kwh']
// ModelChoice is what the user says of the model: null goes back to the recognition.
export type ModelChoice = components['schemas']['VehicleModelUpdate']

// Reading is one value of the state and when it was read: it held at fetched_at and again
// at checked_at.
export type Reading<T = unknown> = Omit<components['schemas']['ReadingFloat64'], 'value'> & {
  value: T
}

export type ChargingStatus = components['schemas']['ReadingChargingStatus']['value']
export type Cable = components['schemas']['ReadingCable']['value']
export type Engine = components['schemas']['ReadingEngine']['value']
