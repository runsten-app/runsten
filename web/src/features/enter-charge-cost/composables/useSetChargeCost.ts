import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { chargeKeys, setChargeCost, type EnteredCost } from '@/entities/charge'
import { afterCostChange } from './invalidate'

// useSetChargeCost enters what was paid for a charge, or enters it again.
export function useSetChargeCost() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (p: { vehicle: string; id: string; cost: EnteredCost }) =>
      setChargeCost(p.vehicle, p.id, p.cost),
    onSuccess: async (charge, p) => {
      queryClient.setQueryData(chargeKeys.detail(p.vehicle, p.id), charge)
      await afterCostChange(queryClient)
    },
  })
  return { setChargeCost: mutateAsync, isPending, error, reset }
}
