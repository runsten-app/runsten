import { useQuery } from '@tanstack/vue-query'
import { connectionKeys, fetchConnection } from '@/entities/connection'

// useConnectionSettings is the account's connection to Volvo: its Volvo ID, and the
// application key its vehicles are read with.
export function useConnectionSettings() {
  const { data, isPending, error } = useQuery({
    queryKey: connectionKeys.current(),
    queryFn: fetchConnection,
  })
  return { connection: data, isPending, error }
}
