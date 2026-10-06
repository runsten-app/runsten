import { useQuery } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { fetchStats, statsKeys, type Bucket, type StatsQuery } from '@/entities/stats'
import { browserTimeZone } from '@/shared/lib'

// useStats reads the statistics of a vehicle over a period (RFC 3339 instants, either
// open), split by bucket when given, in the reader's time zone: the server splits days,
// weeks and months in it, as the lists group their events by the reader's days.
export function useStats(
  vehicle: MaybeRefOrGetter<string>,
  from: MaybeRefOrGetter<string | undefined>,
  to: MaybeRefOrGetter<string | undefined>,
  bucket?: MaybeRefOrGetter<Bucket | undefined>,
) {
  const query = computed<StatsQuery>(() => ({
    from: toValue(from),
    to: toValue(to),
    tz: browserTimeZone(),
    bucket: toValue(bucket),
  }))
  const q = useQuery({
    queryKey: computed(() => statsKeys.period(toValue(vehicle), query.value)),
    queryFn: () => fetchStats(toValue(vehicle), query.value),
  })
  return { stats: q.data, isPending: q.isPending, error: q.error }
}
