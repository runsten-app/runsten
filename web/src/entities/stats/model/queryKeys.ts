import type { StatsQuery } from '../api/statsApi'

export const statsKeys = {
  // The statistics of every vehicle.
  every: () => ['stats'] as const,
  all: (vehicle: string) => ['stats', vehicle] as const,
  period: (vehicle: string, q: StatsQuery) =>
    ['stats', vehicle, q.from ?? null, q.to ?? null, q.tz, q.bucket ?? null] as const,
}
