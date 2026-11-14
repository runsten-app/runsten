import type { QueryClient } from '@tanstack/vue-query'
import { connectionKeys } from '@/entities/connection'
import { vehicleKeys } from '@/entities/vehicle'

// afterSignIn refreshes what a sign-in changes: the connection, and each vehicle's state
// of reading (a sign-in accepted also adds its vehicles).
export async function afterSignIn(queryClient: QueryClient) {
  await Promise.all(
    [connectionKeys.current(), vehicleKeys.list(), vehicleKeys.details()].map((queryKey) =>
      queryClient.invalidateQueries({ queryKey }),
    ),
  )
}
