import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { deleteDemo } from '@/entities/connection'
import { afterDemoChange } from './invalidate'

// useDisconnectDemo deletes the demo vehicle with its whole history.
export function useDisconnectDemo() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: () => deleteDemo(),
    onSuccess: () => afterDemoChange(queryClient),
  })
  return { disconnect: mutateAsync, isPending, error, reset }
}
