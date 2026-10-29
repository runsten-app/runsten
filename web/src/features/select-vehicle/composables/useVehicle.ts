import { useQuery } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { fetchVehicle, vehicleKeys } from '@/entities/vehicle'

// enabled: false waits, as a caller that needs the vehicle only when the list lacks it.
export function useVehicle(
  vehicle: MaybeRefOrGetter<string>,
  enabled: MaybeRefOrGetter<boolean> = true,
) {
  const { data, isPending, error } = useQuery({
    queryKey: computed(() => vehicleKeys.detail(toValue(vehicle))),
    queryFn: () => fetchVehicle(toValue(vehicle)),
    enabled: () => toValue(enabled),
  })
  return { vehicle: data, isPending, error }
}
