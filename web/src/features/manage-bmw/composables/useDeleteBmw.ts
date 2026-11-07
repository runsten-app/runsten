import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { deleteBmw } from '@/entities/connection'
import { afterConnectionChange } from '../model/invalidate'

// useDeleteBmw removes the account's BMW connection, its tokens with it: its cars stay,
// with their history, and are no longer read.
export function useDeleteBmw() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: deleteBmw,
    onSuccess: () => afterConnectionChange(queryClient),
  })
  return { remove: mutateAsync, isPending, error, reset }
}
