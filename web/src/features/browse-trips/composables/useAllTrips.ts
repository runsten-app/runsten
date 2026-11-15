import { useQuery } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { fetchTrips, tripKeys, type PurposeFilter, type Trip } from '@/entities/trip'

// allPages is the largest page the API gives: a month of trips in one or two requests.
const allPages = 200

// useAllTrips reads every trip of a period, of one purpose or all, newest first: what a
// report prints, where a page at a time would not do.
export function useAllTrips(
  vehicle: MaybeRefOrGetter<string>,
  from: MaybeRefOrGetter<string | undefined>,
  to: MaybeRefOrGetter<string | undefined>,
  purpose: MaybeRefOrGetter<PurposeFilter | undefined>,
) {
  const { data, isPending, error } = useQuery({
    queryKey: computed(() => [
      ...tripKeys.list(toValue(vehicle), toValue(from), toValue(to), toValue(purpose)),
      'all',
    ]),
    queryFn: async () => {
      const out: Trip[] = []
      let cursor: string | undefined
      do {
        const page = await fetchTrips(toValue(vehicle), {
          from: toValue(from),
          to: toValue(to),
          purpose: toValue(purpose),
          limit: allPages,
          cursor,
        })
        out.push(...page.items)
        cursor = page.next_cursor ?? undefined
      } while (cursor)
      return out
    },
  })
  return { trips: computed(() => data.value ?? []), isPending, error }
}
