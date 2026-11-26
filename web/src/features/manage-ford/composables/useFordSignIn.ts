import { useMutation } from '@tanstack/vue-query'
import { startFordSignIn } from '@/entities/connection'

// useFordSignIn starts a sign-in on Ford's page with the owner's application: its address,
// whether Ford comes back to this instance by itself (callback), and the state to send
// back with the address the owner pastes when it does not, within 15 minutes. Runsten
// keeps the client ID and the secret meanwhile.
export function useFordSignIn() {
  const { mutateAsync, data, isPending, error, reset } = useMutation({
    mutationFn: (app: { clientId: string; clientSecret: string }) =>
      startFordSignIn(app.clientId, app.clientSecret),
  })
  return { start: mutateAsync, authorization: data, isPending, error, reset }
}
