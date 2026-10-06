import { useQuery } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { fetchPlace, placeKeys } from '@/entities/place'

// usePlace reads a place with its tariff; nothing while place is undefined (a new one).
export function usePlace(place: MaybeRefOrGetter<string | undefined>) {
  const { data, isPending, error } = useQuery({
    queryKey: computed(() => placeKeys.detail(toValue(place) ?? '')),
    queryFn: () => fetchPlace(toValue(place) ?? ''),
    enabled: computed(() => toValue(place) !== undefined),
    // An edited form is not replaced under the user's hands by a refetch.
    refetchOnWindowFocus: false,
  })
  return { place: data, isPending, error }
}
