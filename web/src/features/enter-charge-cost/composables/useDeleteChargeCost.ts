import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { deleteChargeCost } from '@/entities/charge'
import { afterCostChange } from './invalidate'

// useDeleteChargeCost deletes the entered cost of a charge: its tariff's comes back.
export function useDeleteChargeCost() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (p: { vehicle: string; id: string }) => deleteChargeCost(p.vehicle, p.id),
    onSuccess: () => afterCostChange(queryClient),
  })
  return { deleteChargeCost: mutateAsync, isPending, error, reset }
}
