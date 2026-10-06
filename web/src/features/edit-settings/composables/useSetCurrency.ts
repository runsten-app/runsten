import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { chargeKeys } from '@/entities/charge'
import { setCurrency, settingsKeys, type CurrencyCode } from '@/entities/settings'
import { statsKeys } from '@/entities/stats'

export function useSetCurrency() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (currency: CurrencyCode) => setCurrency(currency),
    onSuccess: async (settings) => {
      queryClient.setQueryData(settingsKeys.current(), settings)
      // The costs of the charges, in the lists, the details and the statistics, are in
      // the currency: without one, there is none.
      await Promise.all(
        [chargeKeys.all(), chargeKeys.details(), statsKeys.every()].map((queryKey) =>
          queryClient.invalidateQueries({ queryKey }),
        ),
      )
    },
  })
  return { setCurrency: mutateAsync, isPending, error, reset }
}
