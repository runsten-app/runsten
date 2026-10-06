import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { createPlace, placeKeys, replacePlace, type PlaceFields } from '@/entities/place'
import { afterChange } from './invalidate'

// useSavePlace creates a place (without an ID) or replaces one, tariff included.
export function useSavePlace() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (p: { id?: string; fields: PlaceFields }) =>
      p.id === undefined ? createPlace(p.fields) : replacePlace(p.id, p.fields),
    onSuccess: async (place) => {
      queryClient.setQueryData(placeKeys.detail(place.id), place)
      await afterChange(queryClient)
    },
  })
  return { savePlace: mutateAsync, isPending, error, reset }
}
