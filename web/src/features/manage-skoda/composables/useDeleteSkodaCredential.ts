import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { deleteSkodaCredential } from '@/entities/connection'
import { afterKeyChange } from './invalidate'

// useDeleteSkodaCredential removes the account's MyŠkoda key: its cars stay, with their
// history, and are no longer read.
export function useDeleteSkodaCredential() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: () => deleteSkodaCredential(),
    onSuccess: () => afterKeyChange(queryClient),
  })
  return { deleteCredential: mutateAsync, isPending, error, reset }
}
