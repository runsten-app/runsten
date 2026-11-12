import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { connectionKeys, setSkodaCredential } from '@/entities/connection'
import { afterKeyChange } from './invalidate'

// useSetSkodaCredential gives the account's MyŠkoda key with the VINs it reads. Škoda
// checks it first on each VIN: one it refuses (credential_refused, key_expired), or that
// does not read one of them (vin_not_covered), is not stored.
export function useSetSkodaCredential() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: setSkodaCredential,
    onSuccess: async (connection) => {
      queryClient.setQueryData(connectionKeys.current(), connection)
      await afterKeyChange(queryClient)
    },
  })
  return { setCredential: mutateAsync, isPending, error, reset }
}
