import { keepPreviousData, useQuery } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { fetchSeries, seriesKeys, type SeriesQuery } from '@/entities/series'

// useSeries reads the energy states of a vehicle over a window; none is asked before the
// window is known. A window that moves with the clock keeps showing the previous one
// until the next arrives, rather than an empty chart.
export function useSeries(
  vehicle: MaybeRefOrGetter<string>,
  query: MaybeRefOrGetter<SeriesQuery | undefined>,
) {
  const q = useQuery({
    queryKey: computed(() => {
      const w = toValue(query)
      return w ? seriesKeys.window(toValue(vehicle), w) : seriesKeys.all(toValue(vehicle))
    }),
    queryFn: () => {
      const w = toValue(query)
      if (!w) throw new Error('no window')
      return fetchSeries(toValue(vehicle), w)
    },
    enabled: computed(() => toValue(query) !== undefined),
    placeholderData: keepPreviousData,
  })
  return { series: q.data, isPending: q.isPending, error: q.error }
}
