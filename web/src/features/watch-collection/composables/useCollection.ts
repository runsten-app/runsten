import { useQuery } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { fetchVehicle, fetchVehicles, vehicleKeys } from '@/entities/vehicle'

// The collector writes its status at least every minute: a shorter interval would show
// nothing new. The queries are those of the vehicles, refreshed while watched.
export const collectionRefetchInterval = 60_000

// useCollection watches how a vehicle is read.
export function useCollection(vehicle: MaybeRefOrGetter<string>) {
  const { data, error } = useQuery({
    queryKey: computed(() => vehicleKeys.detail(toValue(vehicle))),
    queryFn: () => fetchVehicle(toValue(vehicle)),
    refetchInterval: collectionRefetchInterval,
  })
  return { vehicle: data, error }
}

// useCollections watches how every vehicle of the account is read.
export function useCollections() {
  const { data, isPending, error } = useQuery({
    queryKey: vehicleKeys.list(),
    queryFn: fetchVehicles,
    refetchInterval: collectionRefetchInterval,
  })
  return { vehicles: data, isPending, error }
}
