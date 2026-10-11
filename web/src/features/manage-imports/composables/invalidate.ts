import type { QueryClient } from '@tanstack/vue-query'
import { batteryKeys } from '@/entities/battery'
import { chargeKeys } from '@/entities/charge'
import { statsKeys } from '@/entities/stats'
import { tripKeys } from '@/entities/trip'
import { importKeys } from '@/entities/vehicle-import'

// afterImportChange reads again what an import or a deletion of one changes: the
// vehicle's imports, its lists, the details and every statistic.
export function afterImportChange(queryClient: QueryClient, vehicle: string) {
  return Promise.all(
    [
      importKeys.list(vehicle),
      tripKeys.lists(vehicle),
      tripKeys.details(),
      chargeKeys.all(),
      chargeKeys.details(),
      statsKeys.every(),
      batteryKeys.every(),
    ].map((queryKey) => queryClient.invalidateQueries({ queryKey })),
  )
}
