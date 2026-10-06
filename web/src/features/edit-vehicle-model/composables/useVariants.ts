import { useQuery } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { fetchVariants, vehicleKeys } from '@/entities/vehicle'

// useVariants are the variants a vehicle may be: the recognition's candidates first.
export function useVariants(vehicle: MaybeRefOrGetter<string>) {
  const { data, isPending, error } = useQuery({
    queryKey: computed(() => vehicleKeys.variants(toValue(vehicle))),
    queryFn: () => fetchVariants(toValue(vehicle)),
  })
  return { variants: data, isPending, error }
}
