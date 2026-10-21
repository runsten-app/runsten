import type { QueryClient } from '@tanstack/vue-query'
import { connectionKeys } from '@/entities/connection'
import { vehicleKeys } from '@/entities/vehicle'

// afterCredentialChange refreshes what a credential changes: the connection, and each
// vehicle's state of reading (a credential accepted also adds its vehicles).
export async function afterCredentialChange(queryClient: QueryClient) {
  await Promise.all(
    [connectionKeys.current(), vehicleKeys.list(), vehicleKeys.details()].map((queryKey) =>
      queryClient.invalidateQueries({ queryKey }),
    ),
  )
}
