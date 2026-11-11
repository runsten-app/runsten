import { useQueries } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { batteryKeys, getBattery, type Battery } from '@/entities/battery'
import { fetchStats, statsKeys, type Stats, type StatsQuery } from '@/entities/stats'
import { fetchVehicleState, vehicleKeys, type State } from '@/entities/vehicle'
import { browserTimeZone } from '@/shared/lib'
import { instants, periodDays, type GaragePeriod } from '../model/garage'

// The collector reads no more often than once a minute while driving or charging.
const stateRefetchInterval = 60_000

// GarageVehicle is what the Garage knows of one vehicle: its state now, its statistics
// over the period, over the last 12 months by month, and its battery, each undefined
// until read (or when not asked for).
export type GarageVehicle = {
  id: string
  state?: State
  period?: Stats
  months?: Stats
  battery?: Battery
}

// useGarage reads every vehicle of the account side by side, in parallel: its state, its
// statistics over the period and over the last 12 months by month (the same read when the
// period is those months), and its battery when asked for (the battery's view). stats:
// false leaves the statistics unread, where the account's limits leave them out.
export function useGarage(
  vehicles: MaybeRefOrGetter<string[]>,
  period: MaybeRefOrGetter<GaragePeriod>,
  options: { stats: MaybeRefOrGetter<boolean>; battery: MaybeRefOrGetter<boolean> },
) {
  const tz = browserTimeZone()
  const today = new Date()
  const months = computed<StatsQuery>(() => ({
    ...instants(periodDays('12m', today)),
    tz,
    bucket: 'month',
  }))
  const over = computed<StatsQuery>(() =>
    toValue(period) === '12m' ? months.value : { ...instants(periodDays('30d', today)), tz },
  )
  const ids = computed(() => toValue(vehicles))
  const statsOn = computed(() => toValue(options.stats))
  const batteryOn = computed(() => toValue(options.battery))

  const states = useQueries({
    queries: computed(() =>
      ids.value.map((id) => ({
        queryKey: vehicleKeys.state(id),
        queryFn: () => fetchVehicleState(id),
        refetchInterval: stateRefetchInterval,
      })),
    ),
  })
  const periods = useQueries({
    queries: computed(() =>
      ids.value.map((id) => ({
        queryKey: statsKeys.period(id, over.value),
        queryFn: () => fetchStats(id, over.value),
        enabled: statsOn.value,
      })),
    ),
  })
  const monthly = useQueries({
    queries: computed(() =>
      ids.value.map((id) => ({
        queryKey: statsKeys.period(id, months.value),
        queryFn: () => fetchStats(id, months.value),
        enabled: statsOn.value,
      })),
    ),
  })
  const batteries = useQueries({
    queries: computed(() =>
      ids.value.map((id) => ({
        queryKey: batteryKeys.trend(id, { tz }),
        queryFn: () => getBattery(id, { tz }),
        enabled: batteryOn.value,
      })),
    ),
  })

  const garage = computed<GarageVehicle[]>(() =>
    ids.value.map((id, i) => ({
      id,
      state: states.value[i]?.data,
      period: periods.value[i]?.data,
      months: monthly.value[i]?.data,
      battery: batteries.value[i]?.data,
    })),
  )
  // Pending: a figure of the period not read yet for some vehicle.
  const isPending = computed(
    () => statsOn.value && periods.value.some((q) => q.isPending && q.fetchStatus !== 'idle'),
  )
  const error = computed(
    () => [...periods.value, ...monthly.value].find((q) => q.error)?.error ?? null,
  )
  return { garage, isPending, error }
}
