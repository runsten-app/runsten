import { useMutation, useQueryClient } from '@tanstack/vue-query'
import {
  connectionKeys,
  setPolestarIdCode,
  setPolestarIdPassword,
  type ConnectionSettings,
} from '@/entities/connection'
import { afterConnectionChange } from './invalidate'

// What connects the Polestar ID: the code of a sign-in on Polestar's page, or the email
// and password.
export type PolestarIdSignIn =
  | { mode: 'token'; state: string; callback: string }
  | { mode: 'password'; email: string; password: string }

function connect(s: PolestarIdSignIn): Promise<ConnectionSettings> {
  return s.mode === 'token'
    ? setPolestarIdCode(s.state, s.callback)
    : setPolestarIdPassword(s.email, s.password)
}

// useConnectPolestarId connects the account's Polestar ID. The Polestar ID checks it
// first: a code or a password it refuses (credential_refused), a sign-in expired
// (authorization_expired), or an account without a car (no_vehicle), is not stored.
export function useConnectPolestarId() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: connect,
    onSuccess: async (connection) => {
      queryClient.setQueryData(connectionKeys.current(), connection)
      await afterConnectionChange(queryClient)
    },
  })
  return { connect: mutateAsync, isPending, error, reset }
}
