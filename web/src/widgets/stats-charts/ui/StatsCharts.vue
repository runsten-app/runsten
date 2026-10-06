<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useTheme } from 'vuetify'
import { useStats, type StatsView } from '@/features/view-stats'
import { majorUnits, useFormat, type Bucket } from '@/shared/lib'
import { ChartFrame, type Series } from '@/shared/ui/chart'

// StatsCharts draws the intervals of the period: distance, energy charged by type,
// consumption, the time spent driving and charging, the loss while parked, and, with the
// account's currency,
// the cost of the charges and what 100 km cost. Pressing an interval leads to its
// events, in the list of trips or of charges: the raw data behind the bar. The view
// keeps those of driving, of charging or of the costs.
const props = defineProps<{
  vehicle: string
  from?: string
  to?: string
  bucket: Bucket
  view: StatsView
}>()
const { t } = useI18n()
const f = useFormat()
const theme = useTheme()
const router = useRouter()
const { stats } = useStats(
  () => props.vehicle,
  () => props.from,
  () => props.to,
  () => props.bucket,
)

// The theme's colors are hex strings once computed.
function color(name: string): string {
  const c = theme.current.value.colors[name]
  return typeof c === 'string' ? c : '#000'
}
const intervals = computed(() => stats.value?.buckets ?? [])
const bucket = computed<Bucket>(() => stats.value?.bucket ?? props.bucket)
const labels = computed(() => intervals.value.map((b) => f.bucket(b.start, bucket.value)))
const intervalHeader = computed(
  () =>
    ({
      day: t('stats.bucket.dayHeader'),
      week: t('stats.bucket.weekHeader'),
      month: t('stats.bucket.monthHeader'),
    })[bucket.value],
)

// open leads to the list of an interval's events: its days, as the period filter keeps
// them, the last one the day before its end.
function open(list: 'trips' | 'charges', index: number) {
  const b = intervals.value[index]
  if (!b) return
  const lastDay = new Date(Date.parse(b.end) - 1).toISOString()
  const query = { from: f.dayKey(b.start), to: f.dayKey(lastDay) }
  return router.push({ name: list, params: { vehicle: props.vehicle }, query })
}

// peak is the largest known value and the label of its interval.
function peak(data: (number | null)[]): { value: number; at: string } | undefined {
  let best: { value: number; at: string } | undefined
  data.forEach((v, i) => {
    if (v !== null && (!best || v > best.value)) best = { value: v, at: labels.value[i] ?? '' }
  })
  return best
}

const distance = computed(() => {
  const data = intervals.value.map((b) => b.trips.distance_km)
  const series: Series[] = [
    { label: t('stats.chart.distance.series'), data, color: color('chart-1') },
  ]
  const top = peak(data)
  const total = stats.value?.totals.trips.distance_km ?? 0
  const summary =
    top && top.value > 0
      ? t('stats.chart.distance.summary', {
          total: f.quantity(total, 'km'),
          max: f.quantity(top.value, 'km'),
          at: top.at,
        })
      : t('stats.chart.distance.none')
  return { series, summary }
})

const energy = computed(() => {
  const by = (k: 'ac' | 'dc' | 'unknown') =>
    intervals.value.map((b) => b.charges.by_type[k].energy_soc_kwh)
  const series: Series[] = [
    { label: t('stats.chart.energy.ac'), data: by('ac'), color: color('chart-1') },
    { label: t('stats.chart.energy.dc'), data: by('dc'), color: color('chart-2') },
  ]
  const types = stats.value?.totals.charges.by_type
  // Reconstructed charges have no type: a third series, so that the bars add up.
  if (types?.unknown.count)
    series.push({
      label: t('stats.chart.energy.unknown'),
      data: by('unknown'),
      color: color('chart-3'),
    })
  const summary =
    types && types.ac.count + types.dc.count + types.unknown.count > 0
      ? t('stats.chart.energy.summary', {
          ac: f.quantity(types.ac.energy_soc_kwh, 'kWh', 0),
          dc: f.quantity(types.dc.energy_soc_kwh, 'kWh', 0),
        })
      : t('stats.chart.energy.none')
  return { series, summary }
})

const consumption = computed(() => {
  const data = intervals.value.map((b) => b.trips.consumption_kwh_per_100km)
  const series: Series[] = [
    { label: t('stats.chart.consumption.series'), data, color: color('chart-1') },
  ]
  const known = data.filter((v): v is number => v !== null)
  const avg = stats.value?.totals.trips.consumption_kwh_per_100km ?? null
  const summary =
    avg !== null && known.length
      ? t('stats.chart.consumption.summary', {
          average: f.quantity(avg, 'kWhPer100km'),
          min: f.quantity(Math.min(...known), 'kWhPer100km'),
          max: f.quantity(Math.max(...known), 'kWhPer100km'),
        })
      : t('stats.chart.consumption.none')
  return { series, summary }
})
// parked is the state of charge lost per day while parked, an average over each
// interval: a gap where no loss is known. No list holds the parked time, so a press
// leads nowhere.
const parked = computed(() => {
  const data = intervals.value.map((b) => b.parked.soc_loss_pct_per_day)
  const series: Series[] = [
    { label: t('stats.chart.parked.series'), data, color: color('chart-1') },
  ]
  const average = stats.value?.totals.parked.soc_loss_pct_per_day ?? null
  const summary =
    average !== null
      ? t('stats.chart.parked.summary', { average: perDay(average) })
      : t('stats.chart.parked.none')
  return { series, summary }
})
function perDay(v: number | null): string {
  return v === null ? f.unknown() : t('stats.tile.perDay', { value: f.quantity(v, 'percent', 1) })
}

// cost stacks the lowest cost of each interval and its uncertainty, up to the highest:
// the bounds of the charges' times allow every amount between. An interval whose costs
// are all unknown has no bar, never a zero.
const cost = computed(() => {
  const currency = stats.value?.currency
  if (!currency) return null
  const d = currency.minor_digits
  const known = intervals.value.map(
    (b) => !b.charges.count || b.charges.cost_unknown < b.charges.count,
  )
  const values = (v: (c: { min_minor: number; max_minor: number }) => number) =>
    intervals.value.map((b, i) => (known[i] ? majorUnits(v(b.charges.cost), d) : null))
  const min = values((c) => c.min_minor)
  const series: Series[] = [
    { label: t('stats.chart.cost.min'), data: min, color: color('chart-1') },
    {
      label: t('stats.chart.cost.uncertainty'),
      data: values((c) => c.max_minor - c.min_minor),
      color: color('chart-1'),
      soft: true,
    },
  ]
  const tableSeries: Series[] = [
    { label: t('stats.chart.cost.minHeader'), data: min, color: color('chart-1') },
    {
      label: t('stats.chart.cost.maxHeader'),
      data: values((c) => c.max_minor),
      color: color('chart-4'),
    },
  ]
  const total = stats.value?.totals.charges
  // The interval with the highest possible cost.
  let top = -1
  intervals.value.forEach((b, i) => {
    const best = intervals.value[top]
    if (known[i] && b.charges.cost.max_minor > (best?.charges.cost.max_minor ?? 0)) top = i
  })
  const peak = intervals.value[top]
  let summary = t('stats.chart.cost.none')
  if (total && total.cost_unknown < total.count) {
    const sum = f.amounts(total.cost, currency).spoken
    summary = peak
      ? t('stats.chart.cost.summary', {
          total: sum,
          max: f.amounts(peak.charges.cost, currency).spoken,
          at: labels.value[top] ?? '',
        })
      : t('stats.chart.cost.total', { total: sum })
  }
  const format = (v: number | null) => (v === null ? f.unknown() : f.majorAmount(v, currency))
  return { series, tableSeries, summary, format, unit: f.currencySymbol(currency.code) }
})

// perHundred is a cost per 100 km, in minor units: the lowest rounded down, the highest
// up, as the API rounds the costs. Unknown when a cost or a distance of its events is:
// a part left out would bend the ratio, not just lower it.
type Ratio = { min_minor: number; max_minor: number }
function perHundred(
  trips: { distance_km: number; distance_unknown: number },
  charges: { count: number; cost: Ratio; cost_unknown: number },
): Ratio | null {
  if (trips.distance_km <= 0 || trips.distance_unknown > 0 || charges.cost_unknown > 0) return null
  return {
    min_minor: Math.floor((charges.cost.min_minor * 100) / trips.distance_km),
    max_minor: Math.ceil((charges.cost.max_minor * 100) / trips.distance_km),
  }
}

// costPerDistance is what each 100 km cost, the charges of an interval over its
// distance: a line of the lowest cost, banded up to the highest. A rate, not a sum: an
// interval without a charge costs nothing, one with a charge but no trip has no rate.
const costPerDistance = computed(() => {
  const currency = stats.value?.currency
  if (!currency) return null
  const d = currency.minor_digits
  const ratios = intervals.value.map((b) => perHundred(b.trips, b.charges))
  const min = ratios.map((r) => (r ? majorUnits(r.min_minor, d) : null))
  const max = ratios.map((r) => (r ? majorUnits(r.max_minor, d) : null))
  const series: Series[] = [
    { label: t('stats.chart.costPerDistance.min'), data: min, color: color('chart-1') },
    {
      label: t('stats.chart.costPerDistance.range'),
      data: max,
      color: color('chart-4'),
      band: true,
    },
  ]
  const tableSeries: Series[] = [
    { label: t('stats.chart.cost.minHeader'), data: min, color: color('chart-1') },
    { label: t('stats.chart.cost.maxHeader'), data: max, color: color('chart-4') },
  ]
  const totals = stats.value?.totals
  const average = totals && perHundred(totals.trips, totals.charges)
  const summary = average
    ? t('stats.chart.costPerDistance.summary', {
        average: f.amounts(average, currency).spoken,
      })
    : t('stats.chart.costPerDistance.none')
  const format = (v: number | null) =>
    v === null
      ? f.unknown()
      : t('stats.chart.costPerDistance.value', { amount: f.majorAmount(v, currency) })
  return {
    series,
    tableSeries,
    summary,
    format,
    unit: t('stats.chart.costPerDistance.unit', { currency: f.currencySymbol(currency.code) }),
  }
})

// time stacks the hours spent driving and charging in each interval, each as its lowest
// and its uncertainty up to the highest: the bounds of the events' times allow every
// duration between. Parked time would dwarf both; it stays in the tiles.
const time = computed(() => {
  const hours = (s: number) => s / 3600
  type Part = { count: number; time: { min: number; max: number }; unknown: number }
  // An interval whose events all have an unknown duration has no bar, never a zero.
  const known = (p: Part) => !p.count || p.unknown < p.count
  const values = (parts: Part[], v: (t: { min: number; max: number }) => number) =>
    parts.map((p) => (known(p) ? hours(v(p.time)) : null))
  const driving = intervals.value.map((b) => ({
    count: b.trips.count,
    time: b.trips.driving_time_s,
    unknown: b.trips.driving_time_unknown,
  }))
  const charging = intervals.value.map((b) => ({
    count: b.charges.count,
    time: b.charges.charging_time_s,
    unknown: b.charges.charging_time_unknown,
  }))
  const series: Series[] = [
    {
      label: t('stats.chart.time.driving'),
      data: values(driving, (d) => d.min),
      color: color('chart-1'),
    },
    {
      label: t('stats.chart.time.drivingUncertainty'),
      data: values(driving, (d) => d.max - d.min),
      color: color('chart-1'),
      soft: true,
    },
    {
      label: t('stats.chart.time.charging'),
      data: values(charging, (d) => d.min),
      color: color('chart-2'),
    },
    {
      label: t('stats.chart.time.chargingUncertainty'),
      data: values(charging, (d) => d.max - d.min),
      color: color('chart-2'),
      soft: true,
    },
  ]
  const tableSeries: Series[] = [
    {
      label: t('stats.chart.time.drivingMin'),
      data: values(driving, (d) => d.min),
      color: color('chart-1'),
    },
    {
      label: t('stats.chart.time.drivingMax'),
      data: values(driving, (d) => d.max),
      color: color('chart-1'),
    },
    {
      label: t('stats.chart.time.chargingMin'),
      data: values(charging, (d) => d.min),
      color: color('chart-2'),
    },
    {
      label: t('stats.chart.time.chargingMax'),
      data: values(charging, (d) => d.max),
      color: color('chart-2'),
    },
  ]
  const totals = stats.value?.totals
  const summary =
    totals && totals.trips.count + totals.charges.count > 0
      ? t('stats.chart.time.summary', {
          driving: f.totalDuration(totals.trips.driving_time_s),
          charging: f.totalDuration(totals.charges.charging_time_s),
        })
      : t('stats.chart.time.none')
  const format = (v: number | null) =>
    v === null ? f.unknown() : f.totalDuration({ min: v * 3600, max: v * 3600 })
  return { series, tableSeries, summary, format }
})
</script>

<template>
  <div v-if="intervals.length" class="stats-charts">
    <ChartFrame
      v-if="view === 'driving'"
      class="distance"
      :hint="t('stats.chart.distance.hint')"
      kind="bar"
      :title="t('stats.chart.distance.title')"
      :summary="distance.summary"
      :labels
      :series="distance.series"
      unit="km"
      :interval-header="intervalHeader"
      :format="(v) => f.quantity(v, 'km')"
      @select="(i: number) => open('trips', i)"
    />
    <ChartFrame
      v-if="view === 'charging'"
      class="energy"
      :hint="t('stats.chart.energy.hint')"
      kind="bar"
      stacked
      :title="t('stats.chart.energy.title')"
      :summary="energy.summary"
      :labels
      :series="energy.series"
      unit="kWh"
      :interval-header="intervalHeader"
      :format="(v) => f.quantity(v, 'kWh', 0)"
      @select="(i: number) => open('charges', i)"
    />
    <ChartFrame
      v-if="view === 'driving'"
      class="consumption"
      :hint="t('stats.chart.consumption.hint')"
      kind="line"
      :title="t('stats.chart.consumption.title')"
      :summary="consumption.summary"
      :labels
      :series="consumption.series"
      unit="kWh/100 km"
      :interval-header="intervalHeader"
      :format="(v) => f.quantity(v, 'kWhPer100km')"
      @select="(i: number) => open('trips', i)"
    />
    <ChartFrame
      v-if="view === 'driving'"
      class="time"
      :hint="t('stats.chart.time.hint')"
      kind="bar"
      stacked
      :title="t('stats.chart.time.title')"
      :summary="time.summary"
      :labels
      :series="time.series"
      :table-series="time.tableSeries"
      unit="h"
      :interval-header="intervalHeader"
      :format="time.format"
      @select="(i: number) => open('trips', i)"
    />
    <ChartFrame
      v-if="view === 'charging'"
      class="parked"
      :hint="t('stats.chart.parked.hint')"
      kind="line"
      :title="t('stats.chart.parked.title')"
      :summary="parked.summary"
      :labels
      :series="parked.series"
      :unit="t('stats.chart.parked.unit')"
      :interval-header="intervalHeader"
      :format="perDay"
      :selectable="false"
    />
    <ChartFrame
      v-if="cost && view === 'costs'"
      class="cost"
      :hint="t('stats.chart.cost.hint')"
      kind="bar"
      stacked
      :title="t('stats.chart.cost.title')"
      :summary="cost.summary"
      :labels
      :series="cost.series"
      :table-series="cost.tableSeries"
      :unit="cost.unit"
      :interval-header="intervalHeader"
      :format="cost.format"
      @select="(i: number) => open('charges', i)"
    />
    <ChartFrame
      v-if="costPerDistance && view === 'costs'"
      class="cost-per-distance"
      :hint="t('stats.chart.costPerDistance.hint')"
      kind="line"
      :title="t('stats.chart.costPerDistance.title')"
      :summary="costPerDistance.summary"
      :labels
      :series="costPerDistance.series"
      :table-series="costPerDistance.tableSeries"
      :unit="costPerDistance.unit"
      :interval-header="intervalHeader"
      :format="costPerDistance.format"
      @select="(i: number) => open('charges', i)"
    />
  </div>
</template>

<style scoped>
.stats-charts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
@media (max-width: 959.98px) {
  .stats-charts {
    grid-template-columns: minmax(0, 1fr);
    gap: 8px;
  }
}
</style>
