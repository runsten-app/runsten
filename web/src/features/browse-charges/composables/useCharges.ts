import { useInfiniteQuery } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { fetchCharges, chargeKeys } from '@/entities/charge'

// useCharges reads the charges of a vehicle in a period (RFC 3339 instants, either open),
// newest first, a page at a time: loadMore asks the next one while there is one.
export function useCharges(
  vehicle: MaybeRefOrGetter<string>,
  from: MaybeRefOrGetter<string | undefined>,
  to: MaybeRefOrGetter<string | undefined>,
) {
  const q = useInfiniteQuery({
    queryKey: computed(() => chargeKeys.list(toValue(vehicle), toValue(from), toValue(to))),
    queryFn: ({ pageParam }) =>
      fetchCharges(toValue(vehicle), { from: toValue(from), to: toValue(to), cursor: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (p) => p.next_cursor ?? undefined,
  })
  return {
    charges: computed(() => q.data.value?.pages.flatMap((p) => p.items) ?? []),
    isPending: q.isPending,
    error: q.error,
    hasMore: q.hasNextPage,
    loadMore: () => q.fetchNextPage(),
    isLoadingMore: q.isFetchingNextPage,
    loadMoreFailed: q.isFetchNextPageError,
  }
}
