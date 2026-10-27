import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { connectionKeys, setTeslaToken } from '@/entities/connection'
import { afterCredentialChange } from './invalidate'

// useSetTeslaToken gives the account's Tesla refresh token. Tesla checks it first: one it
// refuses (credential_refused), whose account the Owner API is closed to (api_closed), or
// that has no vehicle (no_vehicle), is not stored.
export function useSetTeslaToken() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (token: string) => setTeslaToken(token),
    onSuccess: async (connection) => {
      queryClient.setQueryData(connectionKeys.current(), connection)
      await afterCredentialChange(queryClient)
    },
  })
  return { setToken: mutateAsync, isPending, error, reset }
}
