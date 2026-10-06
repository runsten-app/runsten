import { useQuery } from '@tanstack/vue-query'
import { fetchVehicles, vehicleKeys } from '@/entities/vehicle'

export function useVehicles() {
  const { data, isPending, error } = useQuery({
    queryKey: vehicleKeys.list(),
    queryFn: fetchVehicles,
  })
  return { vehicles: data, isPending, error }
}
