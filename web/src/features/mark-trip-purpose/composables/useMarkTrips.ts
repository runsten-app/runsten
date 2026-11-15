import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { statsKeys } from '@/entities/stats'
import { setTripMarks, tripKeys, type Mark } from '@/entities/trip'

// batch is the most trips the API marks in one write.
const batch = 200

// useMarkTrips marks trips with a purpose, none, or gives them back to the places' rules,
// a batch at a time; then refreshes what shows a purpose, the lists, the details and the
// statistics, even after a failure: the batches before it are marked.
export function useMarkTrips() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: async (p: { vehicle: string; trips: string[]; mark: Mark }) => {
      for (let i = 0; i < p.trips.length; i += batch)
        await setTripMarks(p.vehicle, p.trips.slice(i, i + batch), p.mark)
    },
    onSettled: async (_, __, p) => {
      await Promise.all(
        [tripKeys.lists(p.vehicle), tripKeys.details(), statsKeys.every()].map((queryKey) =>
          queryClient.invalidateQueries({ queryKey }),
        ),
      )
    },
  })
  return { markTrips: mutateAsync, isPending, error, reset }
}
