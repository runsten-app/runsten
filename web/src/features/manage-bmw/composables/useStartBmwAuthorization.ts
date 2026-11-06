import { useMutation } from '@tanstack/vue-query'
import { startBmwAuthorization } from '@/entities/connection'

// useStartBmwAuthorization starts BMW's device flow with the client ID the owner created:
// BMW's code and the page to enter it on. BMW checks the client ID first (a client ID it
// does not know is credential_refused). Runsten keeps the flow's secrets, for 10 minutes
// at most.
export function useStartBmwAuthorization() {
  const { mutateAsync, data, isPending, error, reset } = useMutation({
    mutationFn: (clientId: string) => startBmwAuthorization(clientId),
  })
  return { start: mutateAsync, device: data, isPending, error, reset }
}
