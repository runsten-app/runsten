import { useQuery, useQueryClient, type InfiniteData } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { chargeKeys, fetchCharge, type Charge, type ChargePage } from '@/entities/charge'

// useCharge reads a charge. Coming from a list, the charge is already there: it is shown
// at once, and read again only once as stale as that list. Not enabled, it asks nothing:
// the ID of a charge in progress is no charge yet.
export function useCharge(
  vehicle: MaybeRefOrGetter<string>,
  id: MaybeRefOrGetter<string>,
  enabled: MaybeRefOrGetter<boolean> = true,
) {
  const queryClient = useQueryClient()

  function fromLists(): { charge: Charge; updatedAt: number } | undefined {
    const lists = queryClient
      .getQueryCache()
      .findAll({ queryKey: chargeKeys.lists(toValue(vehicle)) })
    for (const q of lists) {
      const data = q.state.data as InfiniteData<ChargePage> | undefined
      const charge = data?.pages.flatMap((p) => p.items).find((c) => c.id === toValue(id))
      if (charge) return { charge, updatedAt: q.state.dataUpdatedAt }
    }
  }

  const { data, isPending, error } = useQuery({
    queryKey: computed(() => chargeKeys.detail(toValue(vehicle), toValue(id))),
    queryFn: () => fetchCharge(toValue(vehicle), toValue(id)),
    initialData: () => fromLists()?.charge,
    initialDataUpdatedAt: () => fromLists()?.updatedAt,
    enabled: computed(() => toValue(enabled)),
  })
  return { charge: data, isPending, error }
}
