import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { connectDemo, connectionKeys } from '@/entities/connection'
import { afterDemoChange } from './invalidate'

// useConnectDemo connects the demo vehicle: the instance enrolls it and writes its last
// four weeks, which takes a few seconds.
export function useConnectDemo() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: connectDemo,
    onSuccess: async (connection) => {
      queryClient.setQueryData(connectionKeys.current(), connection)
      await afterDemoChange(queryClient)
    },
  })
  return { connect: mutateAsync, isPending, error, reset }
}
