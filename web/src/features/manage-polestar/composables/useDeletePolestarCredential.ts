import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { deletePolestarCredential } from '@/entities/connection'
import { afterCredentialChange } from './invalidate'

// useDeletePolestarCredential removes the account's Polestar credential: its vehicles stay,
// with their history, and are no longer read.
export function useDeletePolestarCredential() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: deletePolestarCredential,
    onSuccess: () => afterCredentialChange(queryClient),
  })
  return { deleteCredential: mutateAsync, isPending, error, reset }
}
