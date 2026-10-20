import { useMutation, useQueryClient } from '@tanstack/vue-query'
import {
  connectionKeys,
  setPolestarCredential,
  type PolestarCredentialUpdate,
} from '@/entities/connection'
import { afterCredentialChange } from './invalidate'

// useSetPolestarCredential gives the account's Polestar credential. The Data Portal checks
// it first: one it refuses (credential_refused), or that reads no vehicle (no_vehicle), is
// not stored.
export function useSetPolestarCredential() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (body: PolestarCredentialUpdate) => setPolestarCredential(body),
    onSuccess: async (connection) => {
      queryClient.setQueryData(connectionKeys.current(), connection)
      await afterCredentialChange(queryClient)
    },
  })
  return { setCredential: mutateAsync, isPending, error, reset }
}
