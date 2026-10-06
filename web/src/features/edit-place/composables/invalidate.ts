import type { QueryClient } from '@tanstack/vue-query'
import { chargeKeys } from '@/entities/charge'
import { placeKeys } from '@/entities/place'
import { statsKeys } from '@/entities/stats'

// afterChange refreshes what a change of a place touches: the list of places, the
// charges without a price, and the place and cost of the charges, in the lists, the
// details and the statistics.
export async function afterChange(queryClient: QueryClient) {
  await Promise.all(
    [
      placeKeys.list(),
      placeKeys.unpricedAll(),
      chargeKeys.all(),
      chargeKeys.details(),
      statsKeys.every(),
    ].map((queryKey) => queryClient.invalidateQueries({ queryKey })),
  )
}
