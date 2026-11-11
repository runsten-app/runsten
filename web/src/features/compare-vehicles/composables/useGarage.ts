import { useQueries } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { batteryKeys, getBattery, type Battery } from '@/entities/battery'
import { fetchStats, statsKeys, type Stats, type StatsQuery } from '@/entities/stats'
import { fetchVehicleState, vehicleKeys, type State } from '@/entities/vehicle'
import { browserTimeZone, dayKey, useNow } from '@/shared/lib'
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
// statistics over the period (totals and distributions, no split: what every account
// has) and over the last 12 months by month, and its battery when asked for (the
// battery's view). monthly: false leaves the split unread, where the account's limits
// refuse it. The bounds follow the reader's day: a page left open past midnight moves on.
export function useGarage(
  vehicles: MaybeRefOrGetter<string[]>,
  period: MaybeRefOrGetter<GaragePeriod>,
  options: { monthly: MaybeRefOrGetter<boolean>; battery: MaybeRefOrGetter<boolean> },
) {
  const tz = browserTimeZone()
  const now = useNow(60_000)
  // A new object only when the day changes: the queries' keys stay put within a day.
  const day = computed(() => dayKey(new Date(now.value).toISOString()))
  const today = computed(() => {
    const [y = 0, m = 1, d = 1] = day.value.split('-').map(Number)
    return new Date(y, m - 1, d)
  })
  const months = computed<StatsQuery>(() => ({
    ...instants(periodDays('12m', today.value)),
    tz,
    bucket: 'month',
  }))
  const over = computed<StatsQuery>(() => ({
    ...instants(periodDays(toValue(period), today.value)),
    tz,
  }))
  const ids = computed(() => toValue(vehicles))
  const monthlyOn = computed(() => toValue(options.monthly))
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
      })),
    ),
  })
  const monthly = useQueries({
    queries: computed(() =>
      ids.value.map((id) => ({
        queryKey: statsKeys.period(id, months.value),
        queryFn: () => fetchStats(id, months.value),
        enabled: monthlyOn.value,
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
  const isPending = computed(() => periods.value.some((q) => q.isPending))
  // Any read that failed: a tile without its state, or a comparison without its data,
  // would otherwise pass for one without data.
  const error = computed(
    () =>
      [
        ...states.value,
        ...periods.value,
        ...(monthlyOn.value ? monthly.value : []),
        ...(batteryOn.value ? batteries.value : []),
      ].find((q) => q.error)?.error ?? null,
  )
  return { garage, isPending, error }
}
