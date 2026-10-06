import { useQuery } from '@tanstack/vue-query'
import { fetchPlaces, placeKeys } from '@/entities/place'

export function usePlaces() {
  const { data, isPending, error } = useQuery({
    queryKey: placeKeys.list(),
    queryFn: fetchPlaces,
  })
  return { places: data, isPending, error }
}
