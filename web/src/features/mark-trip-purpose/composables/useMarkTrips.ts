import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { statsKeys } from '@/entities/stats'
import { setTripMarks, tripKeys, type Mark } from '@/entities/trip'

// useMarkTrips marks trips with a purpose, none, or gives them back to the places' rules;
// then refreshes what shows a purpose: the lists, the details and the statistics.
export function useMarkTrips() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (p: { vehicle: string; trips: string[]; mark: Mark }) =>
      setTripMarks(p.vehicle, p.trips, p.mark),
    onSuccess: async (_, p) => {
      await Promise.all(
        [tripKeys.lists(p.vehicle), tripKeys.details(), statsKeys.every()].map((queryKey) =>
          queryClient.invalidateQueries({ queryKey }),
        ),
      )
    },
  })
  return { markTrips: mutateAsync, isPending, error, reset }
}
