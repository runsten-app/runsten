import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { deleteDemo } from '@/entities/connection'
import { afterDemoChange } from './invalidate'

// useDisconnectDemo deletes the demo vehicle with its whole history. The connection and the
// vehicles are read again meanwhile, not awaited: the caller goes on as soon as it is deleted,
// while what shows it may still be on the page; deleted tells it first of all.
export function useDisconnectDemo(deleted?: () => void) {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: () => deleteDemo(),
    onSuccess: () => {
      // Before the connection is read again: what shows the vehicle may go with it.
      deleted?.()
      void afterDemoChange(queryClient)
    },
  })
  return { disconnect: mutateAsync, isPending, error, reset }
}
