import { useQuery, useQueryClient, type InfiniteData } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { fetchTrip, tripKeys, type Trip, type TripPage } from '@/entities/trip'

// useTrip reads a trip. Coming from a list, the trip is already there: it is shown at
// once, and read again only once as stale as that list.
export function useTrip(vehicle: MaybeRefOrGetter<string>, id: MaybeRefOrGetter<string>) {
  const queryClient = useQueryClient()

  function fromLists(): { trip: Trip; updatedAt: number } | undefined {
    const lists = queryClient
      .getQueryCache()
      .findAll({ queryKey: tripKeys.lists(toValue(vehicle)) })
    for (const q of lists) {
      const data = q.state.data as InfiniteData<TripPage> | undefined
      const trip = data?.pages.flatMap((p) => p.items).find((t) => t.id === toValue(id))
      if (trip) return { trip, updatedAt: q.state.dataUpdatedAt }
    }
  }

  const { data, isPending, error } = useQuery({
    queryKey: computed(() => tripKeys.detail(toValue(vehicle), toValue(id))),
    queryFn: () => fetchTrip(toValue(vehicle), toValue(id)),
    initialData: () => fromLists()?.trip,
    initialDataUpdatedAt: () => fromLists()?.updatedAt,
  })
  return { trip: data, isPending, error }
}
