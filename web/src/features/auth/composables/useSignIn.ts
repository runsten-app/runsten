import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { sessionKeys, signIn } from '@/entities/session'

export function useSignIn() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (c: { username: string; password: string }) => signIn(c.username, c.password),
    onSuccess: (session) => {
      // Whatever an earlier session cached belongs to it.
      queryClient.clear()
      queryClient.setQueryData(sessionKeys.current(), session)
    },
  })
  return { signIn: mutateAsync, isPending, error, reset }
}
