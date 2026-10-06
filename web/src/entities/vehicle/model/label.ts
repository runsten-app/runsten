import type { Vehicle } from './Vehicle'

type Labelled = Pick<Vehicle, 'id' | 'vin' | 'model'>

// vehicleLabel names a vehicle as its driver knows it: its recognized variant, else its
// family, with the model year when known; the VIN until the details are read.
export function vehicleLabel(v: Pick<Vehicle, 'vin' | 'model'>): string {
  const name = v.model.variant?.name ?? v.model.family
  if (name === null) return v.vin
  return v.model.model_year === null ? name : `${name} · ${v.model.model_year}`
}

// serialDigits of the VIN tell apart two vehicles of the same model: the serial number.
const serialDigits = 6

// vehicleLabels labels the vehicles of an account, by ID. Two vehicles of the same model
// would read alike: both then end with the last characters of their VIN.
export function vehicleLabels(vehicles: readonly Labelled[]): Map<string, string> {
  const labels = vehicles.map(vehicleLabel)
  const alike = (label: string) => labels.indexOf(label) !== labels.lastIndexOf(label)
  return new Map(
    vehicles.map((v) => {
      const label = vehicleLabel(v)
      return [v.id, alike(label) ? `${label} · ${v.vin.slice(-serialDigits)}` : label]
    }),
  )
}
