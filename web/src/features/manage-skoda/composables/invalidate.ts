import type { QueryClient } from '@tanstack/vue-query'
import { connectionKeys } from '@/entities/connection'
import { vehicleKeys } from '@/entities/vehicle'

// afterKeyChange refreshes what a key changes: the connection, and each vehicle's state
// of reading (a key accepted also adds its vehicles).
export async function afterKeyChange(queryClient: QueryClient) {
  await Promise.all(
    [connectionKeys.current(), vehicleKeys.list(), vehicleKeys.details()].map((queryKey) =>
      queryClient.invalidateQueries({ queryKey }),
    ),
  )
}
