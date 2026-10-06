import type { Stats } from './Stats'

export type DistanceBand = Stats['trips_by_distance']['bands'][number]

// bandOf is the band a distance lies in: at least its min_km, less than its max_km.
export function bandOf(bands: DistanceBand[], km: number): DistanceBand | undefined {
  return bands.find((b) => km >= b.min_km && (b.max_km === null || km < b.max_km))
}
