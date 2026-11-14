import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { deleteRenault } from '@/entities/connection'
import { afterSignIn } from './invalidate'

// useDeleteRenault removes the account's My Renault sign-in: its vehicles stay, with their
// history, and are no longer read.
export function useDeleteRenault() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: () => deleteRenault(),
    onSuccess: () => afterSignIn(queryClient),
  })
  return { remove: mutateAsync, isPending, error, reset }
}
