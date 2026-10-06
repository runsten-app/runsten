import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { deleteApiKey } from '@/entities/connection'
import { afterKeyChange } from './invalidate'

// useDeleteApiKey removes the account's application key: the instance's reads its
// vehicles again, if it has one.
export function useDeleteApiKey() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: deleteApiKey,
    onSuccess: () => afterKeyChange(queryClient),
  })
  return { deleteApiKey: mutateAsync, isPending, error, reset }
}
