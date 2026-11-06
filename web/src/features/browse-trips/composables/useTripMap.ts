import { useQuery } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { fetchTripMap, tripKeys } from '@/entities/trip'

// useTripMap reads where the vehicle went in a period (RFC 3339 instants, either open):
// the spots its trips started and ended at, and the links between them.
export function useTripMap(
  vehicle: MaybeRefOrGetter<string>,
  from: MaybeRefOrGetter<string | undefined>,
  to: MaybeRefOrGetter<string | undefined>,
) {
  const { data, error, isPending } = useQuery({
    queryKey: computed(() => tripKeys.map(toValue(vehicle), toValue(from), toValue(to))),
    queryFn: () => fetchTripMap(toValue(vehicle), { from: toValue(from), to: toValue(to) }),
  })
  return { map: data, error, isPending }
}
