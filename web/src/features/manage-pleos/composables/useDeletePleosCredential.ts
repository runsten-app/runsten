import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { deletePleosCredential, type PleosBrand } from '@/entities/connection'
import { afterKeyChange } from './invalidate'

// useDeletePleosCredential removes the account's key of one brand: its vehicles stay,
// with their history, and are no longer read.
export function useDeletePleosCredential(brand: PleosBrand) {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: () => deletePleosCredential(brand),
    onSuccess: () => afterKeyChange(queryClient),
  })
  return { deleteCredential: mutateAsync, isPending, error, reset }
}
