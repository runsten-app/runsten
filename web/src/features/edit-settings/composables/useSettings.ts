import { useQuery } from '@tanstack/vue-query'
import { fetchSettings, settingsKeys } from '@/entities/settings'

// useSettings is the account's currency, and the currencies it may choose.
export function useSettings() {
  const { data, isPending, error } = useQuery({
    queryKey: settingsKeys.current(),
    queryFn: fetchSettings,
  })
  return { settings: data, isPending, error }
}
