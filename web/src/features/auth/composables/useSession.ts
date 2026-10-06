import { useQuery } from '@tanstack/vue-query'
import { computed } from 'vue'
import { fetchSession, sessionKeys } from '@/entities/session'

// useSession is the signed-in user. The route guard has loaded it before any page.
export function useSession() {
  const { data, isPending } = useQuery({
    queryKey: sessionKeys.current(),
    queryFn: fetchSession,
    staleTime: 5 * 60_000,
    retry: false,
  })
  return { session: data, username: computed(() => data.value?.user.username), isPending }
}
