import { useQuery } from '@tanstack/vue-query'
import { fetchInstance, instanceKeys } from '@/entities/instance'

// useInstance is the release the instance runs, and a newer one: the instance reads it
// once a day at most, an hour is soon enough.
export function useInstance() {
  const { data, isPending, error } = useQuery({
    queryKey: instanceKeys.current(),
    queryFn: fetchInstance,
    staleTime: 60 * 60_000,
  })
  return { instance: data, isPending, error }
}
