import type { QueryClient } from '@tanstack/vue-query'
import { chargeKeys } from '@/entities/charge'
import { placeKeys } from '@/entities/place'
import { statsKeys } from '@/entities/stats'

// afterCostChange refreshes what an entered cost touches: the charges, in the lists and
// the details, the statistics, the orphaned costs (entering a cost for a charge
// replaces the one it took, which may have been attached by its window), and the
// charges without a price, which an entered cost leaves.
export async function afterCostChange(queryClient: QueryClient) {
  await Promise.all(
    [
      chargeKeys.all(),
      chargeKeys.details(),
      chargeKeys.orphans(),
      placeKeys.unpricedAll(),
      statsKeys.every(),
    ].map((queryKey) => queryClient.invalidateQueries({ queryKey })),
  )
}
