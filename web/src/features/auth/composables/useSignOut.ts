import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { useRouter } from 'vue-router'
import { signOut } from '@/entities/session'

export function useSignOut() {
  const queryClient = useQueryClient()
  const router = useRouter()
  const { mutate, isPending } = useMutation({
    mutationFn: signOut,
    // Signed out or not (the session may have ended already), nothing cached stays. An
    // instance with a page named signedOut ends the sign-out there, else at the sign-in.
    onSettled: async () => {
      queryClient.clear()
      await router.push({ name: router.hasRoute('signedOut') ? 'signedOut' : 'login' })
    },
  })
  return { signOut: () => mutate(), isPending }
}
