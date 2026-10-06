import { useQuery } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { fetchUnpricedCharges, placeKeys } from '@/entities/place'

// useUnpricedCharges reads the charges of a place that its tariff leaves without a
// price; nothing while place is undefined (a new one, not yet saved).
export function useUnpricedCharges(place: MaybeRefOrGetter<string | undefined>) {
  const { data } = useQuery({
    queryKey: computed(() => placeKeys.unpriced(toValue(place) ?? '')),
    queryFn: () => fetchUnpricedCharges(toValue(place) ?? ''),
    enabled: computed(() => toValue(place) !== undefined),
  })
  return { unpriced: data }
}
