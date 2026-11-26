import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { connectionKeys, setFordCode } from '@/entities/connection'
import { afterConnectionChange } from './invalidate'

// useConnectFord ends a sign-in whose page the owner pastes back: Ford checks its code
// first. A sign-in expired or a code Ford refuses (authorization_expired), an application
// it refuses (credential_refused), or a sign-in that reads no car (no_vehicle), is not
// stored.
export function useConnectFord() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (s: { state: string; callback: string }) => setFordCode(s.state, s.callback),
    onSuccess: async (connection) => {
      queryClient.setQueryData(connectionKeys.current(), connection)
      await afterConnectionChange(queryClient)
    },
  })
  return { connect: mutateAsync, isPending, error, reset }
}
