import { useQuery } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { fetchVehicle, vehicleKeys } from '@/entities/vehicle'

export function useVehicle(vehicle: MaybeRefOrGetter<string>) {
  const { data, isPending, error } = useQuery({
    queryKey: computed(() => vehicleKeys.detail(toValue(vehicle))),
    queryFn: () => fetchVehicle(toValue(vehicle)),
  })
  return { vehicle: data, isPending, error }
}
