import { useQuery } from '@tanstack/vue-query'
import { computed, type ComputedRef } from 'vue'
import { fetchSession, sessionKeys, type Session } from '@/entities/session'

// Flag is a feature flag the front end reads: those the API gives with the session.
export type Flag = keyof Session['flags']

// useFlag is a feature flag, as runsten-api evaluated it for the session's account (the
// route guard loaded it before any page): the browser evaluates nothing. Off until the
// session is known, so that nothing shows only to vanish.
export function useFlag(name: Flag): ComputedRef<boolean> {
  const { data } = useQuery({
    queryKey: sessionKeys.current(),
    queryFn: fetchSession,
    staleTime: 5 * 60_000,
    retry: false,
  })
  return computed(() => data.value?.flags[name] ?? false)
}
