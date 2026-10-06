import { useQuery } from '@tanstack/vue-query'
import { chargeKeys, fetchOrphanCosts } from '@/entities/charge'

// useOrphanCosts lists the entered costs no charge takes any more.
export function useOrphanCosts() {
  const { data, isPending, error } = useQuery({
    queryKey: chargeKeys.orphans(),
    queryFn: fetchOrphanCosts,
  })
  return { orphans: data, isPending, error }
}
