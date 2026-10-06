import { useQuery } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { fetchVehicleState, vehicleKeys } from '@/entities/vehicle'

// The collector reads no more often than once a minute while driving or charging: a
// shorter interval would show nothing new. TanStack pauses it while the tab is hidden,
// and refetches when it comes back.
export const stateRefetchInterval = 60_000

export function useVehicleState(vehicle: MaybeRefOrGetter<string>) {
  const { data, isPending, error, isFetching } = useQuery({
    queryKey: computed(() => vehicleKeys.state(toValue(vehicle))),
    queryFn: () => fetchVehicleState(toValue(vehicle)),
    refetchInterval: stateRefetchInterval,
  })
  return { state: data, isPending, error, isFetching }
}
