import { useQuery } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { fetchParked, statsKeys } from '@/entities/stats'

// useParked reads the stops of a vehicle over a period (RFC 3339 instants, either open):
// the parked intervals the statistics' loss while parked sums, newest first. Not read
// while enabled is false: a page that has no period yet.
export function useParked(
  vehicle: MaybeRefOrGetter<string>,
  from: MaybeRefOrGetter<string | undefined>,
  to: MaybeRefOrGetter<string | undefined>,
  enabled: MaybeRefOrGetter<boolean> = true,
) {
  const q = useQuery({
    queryKey: computed(() => statsKeys.parked(toValue(vehicle), toValue(from), toValue(to))),
    queryFn: () => fetchParked(toValue(vehicle), { from: toValue(from), to: toValue(to) }),
    enabled: computed(() => toValue(enabled)),
  })
  return { parked: q.data, isPending: q.isPending, error: q.error }
}
