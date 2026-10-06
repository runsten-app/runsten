import type { QueryClient } from '@tanstack/vue-query'
import { chargeKeys } from '@/entities/charge'
import { placeKeys } from '@/entities/place'
import { statsKeys } from '@/entities/stats'

// afterOrphanChange refreshes what attaching or deleting an orphaned cost touches: the
// orphans, the charges (one takes the cost), their candidates, the statistics, and the
// charges without a price (an attached cost gives one its price).
export async function afterOrphanChange(queryClient: QueryClient) {
  await Promise.all(
    [
      chargeKeys.orphans(),
      chargeKeys.all(),
      chargeKeys.details(),
      chargeKeys.allCandidates(),
      placeKeys.unpricedAll(),
      statsKeys.every(),
    ].map((queryKey) => queryClient.invalidateQueries({ queryKey })),
  )
}
