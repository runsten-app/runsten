import type { QueryClient } from '@tanstack/vue-query'
import { chargeKeys } from '@/entities/charge'
import { placeKeys } from '@/entities/place'
import { statsKeys } from '@/entities/stats'
import { tripKeys } from '@/entities/trip'

// afterChange refreshes what a change of a place touches: the list of places, the
// charges without a price, and the place and cost of the charges, in the lists, the
// details and the statistics, and the spots of the trip maps.
export async function afterChange(queryClient: QueryClient) {
  await Promise.all(
    [
      placeKeys.list(),
      placeKeys.unpricedAll(),
      chargeKeys.all(),
      chargeKeys.details(),
      statsKeys.every(),
      tripKeys.allMaps(),
    ].map((queryKey) => queryClient.invalidateQueries({ queryKey })),
  )
}
