import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { deletePlace, placeKeys } from '@/entities/place'
import { afterChange } from './invalidate'

export function useDeletePlace() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (place: string) => deletePlace(place),
    onSuccess: async (_, place) => {
      queryClient.removeQueries({ queryKey: placeKeys.detail(place) })
      await afterChange(queryClient)
    },
  })
  return { deletePlace: mutateAsync, isPending, error, reset }
}
