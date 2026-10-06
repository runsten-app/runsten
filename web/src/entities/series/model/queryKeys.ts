import type { SeriesQuery } from '../api/seriesApi'

export const seriesKeys = {
  all: (vehicle: string) => ['series', vehicle] as const,
  window: (vehicle: string, q: SeriesQuery) => ['series', vehicle, q.from, q.to ?? null] as const,
}
