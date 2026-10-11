import { useQuery } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { fetchTripTrack, tripKeys } from '@/entities/trip'
import { isApiError } from '@/shared/api'
import type { Position } from '@/shared/lib'

// useTripTrack reads the track of an imported trip, only when enabled: fine-grained
// location data, read to be drawn and never before. A trip without one has none (null).
export function useTripTrack(
  vehicle: MaybeRefOrGetter<string>,
  id: MaybeRefOrGetter<string>,
  enabled: MaybeRefOrGetter<boolean>,
) {
  const { data } = useQuery({
    queryKey: computed(() => tripKeys.track(toValue(vehicle), toValue(id))),
    queryFn: async () => {
      try {
        return await fetchTripTrack(toValue(vehicle), toValue(id))
      } catch (e) {
        if (isApiError(e, 'not_found')) return null
        throw e
      }
    },
    enabled: computed(() => toValue(enabled)),
    // A track changes only with an import, which invalidates every trip and its track.
    staleTime: Infinity,
  })
  // GeoJSON gives the longitude first.
  const line = computed<Position[]>(
    () =>
      data.value?.coordinates.flatMap(([lon, lat]) =>
        lon === undefined || lat === undefined ? [] : [{ lat, lon }],
      ) ?? [],
  )
  return { line }
}
