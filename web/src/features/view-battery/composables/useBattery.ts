import { useQuery } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { batteryKeys, getBattery, type BatteryQuery } from '@/entities/battery'
import { browserTimeZone } from '@/shared/lib'

// useBattery reads the estimated capacity of a vehicle over its whole history, in the
// reader's time zone: the server splits the months in it, as the lists group the events
// by the reader's days.
export function useBattery(vehicle: MaybeRefOrGetter<string>) {
  const query = computed<BatteryQuery>(() => ({ tz: browserTimeZone() }))
  const q = useQuery({
    queryKey: computed(() => batteryKeys.trend(toValue(vehicle), query.value)),
    queryFn: () => getBattery(toValue(vehicle), query.value),
  })
  return { battery: q.data, isPending: q.isPending, error: q.error }
}
