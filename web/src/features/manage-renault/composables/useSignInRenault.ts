import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { connectionKeys, signInRenault } from '@/entities/connection'
import { afterSignIn } from './invalidate'

// useSignInRenault signs the owner in to My Renault with the email and password, which
// travel once and are never stored. Renault answers with the connection, or, for an
// account that asks for it, with the masked address its one-time code was sent to
// (code_sent_to): useRenaultCode then completes the sign-in. Renault checks it first: an
// email or password it refuses (credential_refused), an account without an electric
// vehicle (no_vehicle) or with too many (too_many_vehicles), is not stored.
export function useSignInRenault() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: signInRenault,
    onSuccess: async (answer) => {
      if (!answer.connection) return
      queryClient.setQueryData(connectionKeys.current(), answer.connection)
      await afterSignIn(queryClient)
    },
  })
  return { signIn: mutateAsync, isPending, error, reset }
}
