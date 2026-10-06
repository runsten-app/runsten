import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { connectionKeys, setApiKey } from '@/entities/connection'
import { afterKeyChange } from './invalidate'

// useSetApiKey gives the account's application key. With a Volvo ID connected, Volvo
// checks it first: a key it refuses is not stored (api_key_refused).
export function useSetApiKey() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (key: string) => setApiKey(key),
    onSuccess: async (connection) => {
      queryClient.setQueryData(connectionKeys.current(), connection)
      await afterKeyChange(queryClient)
    },
  })
  return { setApiKey: mutateAsync, isPending, error, reset }
}
