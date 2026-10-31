import { useQuery } from '@tanstack/vue-query'
import { fetchPlaces, placeKeys } from '@/entities/place'

// usePlaces reads the account's places, to choose one.
export function usePlaces() {
  const { data, isPending } = useQuery({ queryKey: placeKeys.list(), queryFn: fetchPlaces })
  return { places: data, isPending }
}
