import type { BatteryQuery } from '../api/batteryApi'

export const batteryKeys = {
  // The battery of every vehicle.
  every: () => ['battery'] as const,
  all: (vehicle: string) => ['battery', vehicle] as const,
  trend: (vehicle: string, q: BatteryQuery) => ['battery', vehicle, q.tz] as const,
}
