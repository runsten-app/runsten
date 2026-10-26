import { keepPreviousData, useQuery } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { fetchHealth, fetchTyres, healthKeys, type TyresQuery } from '@/entities/health'

// useHealth reads the latest health of a vehicle, while enabled. The collector reads it
// every hour: read again every five minutes, a new reading shows soon enough.
export function useHealth(
  vehicle: MaybeRefOrGetter<string>,
  enabled: MaybeRefOrGetter<boolean> = true,
) {
  const q = useQuery({
    queryKey: computed(() => healthKeys.current(toValue(vehicle))),
    queryFn: () => fetchHealth(toValue(vehicle)),
    enabled: computed(() => toValue(enabled)),
    refetchInterval: 5 * 60_000,
  })
  return { health: q.data, isPending: q.isPending, error: q.error }
}

// useTyres reads the tyres' pressures of a vehicle over a window; another window keeps
// showing the previous one until it arrives, rather than an empty chart.
export function useTyres(vehicle: MaybeRefOrGetter<string>, query: MaybeRefOrGetter<TyresQuery>) {
  const q = useQuery({
    queryKey: computed(() => healthKeys.tyres(toValue(vehicle), toValue(query))),
    queryFn: () => fetchTyres(toValue(vehicle), toValue(query)),
    placeholderData: keepPreviousData,
  })
  return { tyres: q.data, isPending: q.isPending, error: q.error }
}
