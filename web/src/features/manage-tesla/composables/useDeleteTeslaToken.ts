import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { deleteTeslaToken } from '@/entities/connection'
import { afterCredentialChange } from './invalidate'

// useDeleteTeslaToken removes the account's Tesla refresh token: its vehicles stay, with
// their history, and are no longer read.
export function useDeleteTeslaToken() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: deleteTeslaToken,
    onSuccess: () => afterCredentialChange(queryClient),
  })
  return { deleteToken: mutateAsync, isPending, error, reset }
}
