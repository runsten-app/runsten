import { useMutation } from '@tanstack/vue-query'
import { startPolestarIdSignIn } from '@/entities/connection'

// usePolestarIdSignIn starts a sign-in on Polestar's page: its address, and the state to
// send back with the address the owner pastes, within 10 minutes. Runsten keeps its PKCE
// verifier.
export function usePolestarIdSignIn() {
  const { mutateAsync, data, isPending, error, reset } = useMutation({
    mutationFn: startPolestarIdSignIn,
  })
  return { start: mutateAsync, authorization: data, isPending, error, reset }
}
