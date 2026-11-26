import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { deleteFord } from '@/entities/connection'
import { afterConnectionChange } from './invalidate'

// useDeleteFord removes the account's Ford connection, the application's secret with it:
// its car stays, with its history, and is no longer read.
export function useDeleteFord() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: deleteFord,
    onSuccess: () => afterConnectionChange(queryClient),
  })
  return { remove: mutateAsync, isPending, error, reset }
}
