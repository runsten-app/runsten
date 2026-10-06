import { QueryClient } from '@tanstack/vue-query'
import { isApiError } from '@/shared/api'

export function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        // Once more after a network or server error; a 4xx will not change by retrying.
        retry: (failures, e) =>
          failures < 1 && !(isApiError(e) && e.status >= 400 && e.status < 500),
      },
    },
  })
}
