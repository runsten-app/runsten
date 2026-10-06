import type { QueryClient } from '@tanstack/vue-query'
import { connectionKeys } from '@/entities/connection'
import { vehicleKeys } from '@/entities/vehicle'

// afterKeyChange refreshes what a key changes: the connection, and each vehicle's key
// and collection (a key accepted may also have recorded the vehicles).
export async function afterKeyChange(queryClient: QueryClient) {
  await Promise.all(
    [connectionKeys.current(), vehicleKeys.list(), vehicleKeys.details()].map((queryKey) =>
      queryClient.invalidateQueries({ queryKey }),
    ),
  )
}
