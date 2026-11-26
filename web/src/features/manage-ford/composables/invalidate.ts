import type { QueryClient } from '@tanstack/vue-query'
import { connectionKeys } from '@/entities/connection'
import { vehicleKeys } from '@/entities/vehicle'

// afterConnectionChange refreshes what a Ford connection changes: the connection, and each
// vehicle's state of reading (a sign-in accepted also adds its car).
export async function afterConnectionChange(queryClient: QueryClient) {
  await Promise.all(
    [connectionKeys.current(), vehicleKeys.list(), vehicleKeys.details()].map((queryKey) =>
      queryClient.invalidateQueries({ queryKey }),
    ),
  )
}
