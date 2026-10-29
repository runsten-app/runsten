import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, toValue, watch, type MaybeRefOrGetter } from 'vue'
import { chargeKeys, fetchChargeInProgress } from '@/entities/charge'
import { statsKeys } from '@/entities/stats'

// The collector writes once a minute while charging: a shorter interval would show
// nothing new. TanStack pauses it while the tab is hidden.
export const chargeRefetchInterval = 60_000

// useChargeInProgress reads the charge in progress of a vehicle, again every minute. Once
// it ends, it is a charge of the lists, under the same ID: they, its details and the
// statistics are read again. Not enabled, it asks nothing: a vehicle unplugged has none.
export function useChargeInProgress(
  vehicle: MaybeRefOrGetter<string>,
  enabled: MaybeRefOrGetter<boolean> = true,
) {
  const queryClient = useQueryClient()
  const { data, isPending, error } = useQuery({
    queryKey: computed(() => chargeKeys.inProgress(toValue(vehicle))),
    queryFn: () => fetchChargeInProgress(toValue(vehicle)),
    refetchInterval: chargeRefetchInterval,
    enabled: computed(() => toValue(enabled)),
  })
  watch(
    () => data.value?.id,
    (id, ended) => {
      if (!ended || id === ended) return
      const v = toValue(vehicle)
      void queryClient.invalidateQueries({ queryKey: chargeKeys.lists(v) })
      void queryClient.invalidateQueries({ queryKey: chargeKeys.detail(v, ended) })
      void queryClient.invalidateQueries({ queryKey: statsKeys.all(v) })
    },
  )
  return { charge: data, isPending, error }
}
