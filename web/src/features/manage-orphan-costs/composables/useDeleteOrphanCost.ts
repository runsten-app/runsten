import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { deleteOrphanCost } from '@/entities/charge'
import { afterOrphanChange } from './invalidate'

export function useDeleteOrphanCost() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (orphan: string) => deleteOrphanCost(orphan),
    onSuccess: () => afterOrphanChange(queryClient),
  })
  return { deleteOrphanCost: mutateAsync, isPending, error, reset }
}
