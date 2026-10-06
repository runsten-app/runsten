import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { attachOrphanCost } from '@/entities/charge'
import { afterOrphanChange } from './invalidate'

// useAttachOrphanCost gives an orphaned cost to a charge; refused (409 charge_has_cost)
// when the charge takes another entered cost.
export function useAttachOrphanCost() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (p: { orphan: string; vehicle: string; charge: string }) =>
      attachOrphanCost(p.orphan, p.vehicle, p.charge),
    onSuccess: () => afterOrphanChange(queryClient),
  })
  return { attachOrphanCost: mutateAsync, isPending, error, reset }
}
