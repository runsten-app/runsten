import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { connectionKeys, setPleosCredential, type PleosBrand } from '@/entities/connection'
import { afterKeyChange } from './invalidate'

// useSetPleosCredential gives the account's key of one brand. The Pleos Playground
// checks it first: one it refuses (credential_refused), or that reads no vehicle
// (no_vehicle), is not stored.
export function useSetPleosCredential(brand: PleosBrand) {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (body: Parameters<typeof setPleosCredential>[1]) => setPleosCredential(brand, body),
    onSuccess: async (connection) => {
      queryClient.setQueryData(connectionKeys.current(), connection)
      await afterKeyChange(queryClient)
    },
  })
  return { setCredential: mutateAsync, isPending, error, reset }
}
