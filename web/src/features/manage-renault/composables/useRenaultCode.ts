import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { connectionKeys, setRenaultCode } from '@/entities/connection'
import { afterSignIn } from './invalidate'

// useRenaultCode enters the one-time code Renault emailed, within ten minutes of the
// sign-in. A wrong code (wrong_code) leaves the sign-in waiting; any other refusal ends
// it (authorization_expired: the ten minutes passed, or the instance restarted).
export function useRenaultCode() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: setRenaultCode,
    onSuccess: async (answer) => {
      if (!answer.connection) return
      queryClient.setQueryData(connectionKeys.current(), answer.connection)
      await afterSignIn(queryClient)
    },
  })
  return { enterCode: mutateAsync, isPending, error, reset }
}
