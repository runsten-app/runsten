import { useQuery } from '@tanstack/vue-query'
import { computed } from 'vue'
import { fetchSession, sessionKeys } from '@/entities/session'

export type Feature = 'costs' | 'stats' | 'csv' | 'mqtt'

// useLimits is what the account's offer leaves out, as the session tells it (the route
// guard loaded it before any page): the history before a day, functions, and vehicles
// it does not read. None where the instance limits no account.
export function useLimits() {
  const { data } = useQuery({
    queryKey: sessionKeys.current(),
    queryFn: fetchSession,
    staleTime: 5 * 60_000,
    retry: false,
  })
  const limits = computed(() => data.value?.limits ?? null)
  return {
    historyFrom: computed(() => limits.value?.history_from ?? null),
    lacks: (f: Feature) => limits.value?.unavailable.includes(f) ?? false,
    unread: (vehicle: string) => limits.value?.unread_vehicles.includes(vehicle) ?? false,
  }
}
