import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { deletePolestarId } from '@/entities/connection'
import { afterConnectionChange } from './invalidate'

// useDeletePolestarId removes the account's Polestar ID, its password with it: its cars
// stay, with their history, and are no longer read.
export function useDeletePolestarId() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: deletePolestarId,
    onSuccess: () => afterConnectionChange(queryClient),
  })
  return { remove: mutateAsync, isPending, error, reset }
}
