<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTheme } from 'vuetify'
import { useBandLabel, useStats, type StatsView } from '@/features/view-stats'
import { majorUnits, useFormat, type Bucket } from '@/shared/lib'
import { ChartFrame, type Series } from '@/shared/ui/chart'
import type { Stats } from '@/entities/stats'

// StatsDistributions draws how the period's trips and charges spread, whatever its
// split: the trips by distance and what each band consumed, the charges by the state of
// charge they started and ended at, and where they took place, with what they cost
// there. A band holds events of the whole period, no list filters by it: a press leads
// nowhere. The view keeps those of driving, of charging or of the costs.
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
const bandLabel = useBandLabel()
// The same query as the charts by interval: one request for the page.
const { stats } = useStats(
  () => props.vehicle,
  () => props.from,
  () => props.to,
  () => props.bucket,
)

function color(name: string): string {
  const c = theme.current.value.colors[name]
  return typeof c === 'string' ? c : '#000'
}
const count = (v: number | null) => (v === null ? f.unknown() : f.number(v))
const sum = (xs: number[]) => xs.reduce((a, b) => a + b, 0)

const bands = computed(() => stats.value?.trips_by_distance.bands ?? [])
const bandLabels = computed(() => bands.value.map(bandLabel))
const tripsLeftOut = computed(() => {
  const n = stats.value?.trips_by_distance.left_out ?? 0
  return n ? t('stats.chart.byDistance.leftOut', n) : undefined
})

const byDistance = computed(() => {
  const data = bands.value.map((b) => b.count)
  const series: Series[] = [
    { label: t('stats.chart.byDistance.series'), data, color: color('chart-1') },
  ]
  const n = sum(data)
  let top = 0
  data.forEach((v, i) => {
    if (v > (data[top] ?? 0)) top = i
  })
  const summary = n
    ? t('stats.chart.byDistance.summary', {
        band: bandLabels.value[top] ?? '',
        count: f.number(data[top] ?? 0),
        n: f.number(n),
      })
    : t('stats.chart.byDistance.none')
  return { series, summary, empty: n ? undefined : summary }
})

// byDistanceConsumption is what the trips of each band consumed: the short ones, on a
// cold battery and in town, often more. A band without a known consumption has no bar.
const byDistanceConsumption = computed(() => {
  const data = bands.value.map((b) => b.consumption_kwh_per_100km)
  const series: Series[] = [
    { label: t('stats.chart.byDistanceConsumption.series'), data, color: color('chart-2') },
  ]
  const known = bands.value.flatMap((b, i) =>
    b.consumption_kwh_per_100km === null
      ? []
      : [
          t('stats.chart.byDistanceConsumption.item', {
            band: bandLabels.value[i] ?? '',
            value: f.quantity(b.consumption_kwh_per_100km, 'kWhPer100km'),
          }),
        ],
  )
  const summary = known.length
    ? t('stats.chart.byDistanceConsumption.summary', { list: known.join('; ') })
    : t('stats.chart.byDistanceConsumption.none')
  return { series, summary, empty: known.length ? undefined : summary }
})

// The bands of the state of charge: ten points each, the last one holding 100 %.
const socBands = Array.from({ length: 10 }, (_, i) => i * 10)
const socLabels = computed(() =>
  socBands.map((lo) =>
    t('stats.band.soc', { min: f.number(lo), max: f.quantity(lo + 10, 'percent') }),
  ),
)
// banded sums the charges of each point into its band.
function banded(points: number[]): number[] {
  return socBands.map((lo) => sum(points.slice(lo, lo === 90 ? 101 : lo + 10)))
}

const bySoC = computed(() => {
  const s = stats.value?.charges_by_soc
  const start = s?.start ?? []
  const end = s?.end ?? []
  const series: Series[] = [
    { label: t('stats.chart.bySoC.started'), data: banded(start), color: color('chart-1') },
    { label: t('stats.chart.bySoC.ended'), data: banded(end), color: color('chart-2') },
  ]
  const n = sum(start)
  // Told from the points themselves: a charge ending at exactly 80 % is not above it.
  const summary = n
    ? t('stats.chart.bySoC.summary', {
        n: f.number(n),
        below: f.number(sum(start.slice(0, 20))),
        above: f.number(sum(end.slice(81))),
      })
    : t('stats.chart.bySoC.none')
  const leftOut = s?.left_out ?? 0
  const hint = [
    t('stats.chart.bySoC.hint'),
    ...(leftOut ? [t('stats.chart.bySoC.leftOut', leftOut)] : []),
  ].join(' ')
  return { series, summary, hint, empty: n ? undefined : summary }
})

// The groups of charges by place: the places, the most energy first, then outside every
// place by type, then without a position; a group without a charge is left out.
type Group = Stats['charges_by_place']['no_position']
const places = computed(() => {
  const p = stats.value?.charges_by_place
  if (!p) return []
  const named = [...p.places]
    .sort((a, b) => b.energy_soc_kwh - a.energy_soc_kwh)
    .map((g) => ({ label: g.place.name, group: g }))
  const others = [
    { label: t('stats.chart.byPlace.outsideAC'), group: p.outside.ac },
    { label: t('stats.chart.byPlace.outsideDC'), group: p.outside.dc },
    { label: t('stats.chart.byPlace.outsideUnknown'), group: p.outside.unknown },
    { label: t('stats.chart.byPlace.noPosition'), group: p.no_position },
  ].filter((g) => g.group.count > 0)
  return [...named, ...others]
})
const placeLabels = computed(() => places.value.map((p) => p.label))

// byPlace is the energy charged at each place: a group whose energies are all unknown
// has no bar, never a zero. The table adds how many charges each holds.
const byPlace = computed(() => {
  const data = places.value.map(({ group: g }) =>
    g.energy_soc_unknown < g.count ? g.energy_soc_kwh : null,
  )
  const series: Series[] = [
    { label: t('stats.chart.byPlace.series'), data, color: color('chart-1') },
  ]
  const tableSeries: Series[] = [
    ...series,
    {
      label: t('stats.chart.byPlace.charges'),
      data: places.value.map((p) => p.group.count),
      color: color('chart-1'),
      format: count,
    },
  ]
  let top = -1
  data.forEach((v, i) => {
    if (v !== null && v > (data[top] ?? 0)) top = i
  })
  const summary =
    top >= 0
      ? t('stats.chart.byPlace.summary', {
          total: f.quantity(sum(data.map((v) => v ?? 0)), 'kWh', 0),
          at: placeLabels.value[top] ?? '',
          max: f.quantity(data[top] ?? 0, 'kWh', 0),
        })
      : t('stats.chart.byPlace.none')
  const reconstructed = sum(places.value.map((p) => p.group.reconstructed))
  const hint = [
    t('stats.chart.byPlace.hint'),
    ...(reconstructed ? [t('stats.chart.byPlace.reconstructed', reconstructed)] : []),
  ].join(' ')
  return { series, tableSeries, summary, hint, empty: top >= 0 ? undefined : summary }
})

// costByPlace stacks the lowest cost of each place and its uncertainty, up to the
// highest, as the cost by interval; only with the account's currency. A group whose
// costs are all unknown has no bar: outside every place, only an entered cost is known.
const costByPlace = computed(() => {
  const currency = stats.value?.currency
  if (!currency) return null
  const d = currency.minor_digits
  const known = places.value.map(({ group: g }) => g.cost_unknown < g.count)
  const values = (v: (c: Group['cost']) => number) =>
    places.value.map((p, i) => (known[i] ? majorUnits(v(p.group.cost), d) : null))
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
  const items = places.value.flatMap((p, i) =>
    known[i]
      ? [
          t('stats.chart.costByPlace.item', {
            place: p.label,
            amount: f.amounts(p.group.cost, currency).spoken,
          }),
        ]
      : [],
  )
  const summary = items.length
    ? t('stats.chart.costByPlace.summary', { list: items.join('; ') })
    : t('stats.chart.costByPlace.none')
  const format = (v: number | null) => (v === null ? f.unknown() : f.majorAmount(v, currency))
  return {
    series,
    tableSeries,
    summary,
    format,
    unit: f.currencySymbol(currency.code),
    empty: items.length ? undefined : summary,
  }
})
</script>

<template>
  <div v-if="stats" class="stats-distributions">
    <ChartFrame
      v-if="view === 'driving'"
      class="by-distance"
      :hint="tripsLeftOut"
      kind="bar"
      :title="t('stats.chart.byDistance.title')"
      :summary="byDistance.summary"
      :empty="byDistance.empty"
      :labels="bandLabels"
      :series="byDistance.series"
      :unit="t('stats.chart.byDistance.unit')"
      :interval-header="t('stats.chart.byDistance.header')"
      :format="count"
      :selectable="false"
    />
    <ChartFrame
      v-if="view === 'driving'"
      class="by-distance-consumption"
      :hint="t('stats.chart.byDistanceConsumption.hint')"
      kind="bar"
      :title="t('stats.chart.byDistanceConsumption.title')"
      :summary="byDistanceConsumption.summary"
      :empty="byDistanceConsumption.empty"
      :labels="bandLabels"
      :series="byDistanceConsumption.series"
      unit="kWh/100 km"
      :interval-header="t('stats.chart.byDistance.header')"
      :format="(v) => f.quantity(v, 'kWhPer100km')"
      :selectable="false"
    />
    <ChartFrame
      v-if="view === 'charging'"
      class="by-soc"
      :hint="bySoC.hint"
      kind="bar"
      :title="t('stats.chart.bySoC.title')"
      :summary="bySoC.summary"
      :empty="bySoC.empty"
      :labels="socLabels"
      :series="bySoC.series"
      :unit="t('stats.chart.bySoC.unit')"
      :interval-header="t('stats.chart.bySoC.header')"
      :format="count"
      :selectable="false"
    />
    <ChartFrame
      v-if="view === 'charging'"
      class="by-place"
      :hint="byPlace.hint"
      kind="bar"
      :title="t('stats.chart.byPlace.title')"
      :summary="byPlace.summary"
      :empty="byPlace.empty"
      :labels="placeLabels"
      :series="byPlace.series"
      :table-series="byPlace.tableSeries"
      unit="kWh"
      :interval-header="t('stats.chart.byPlace.header')"
      :format="(v) => f.quantity(v, 'kWh', 0)"
      :selectable="false"
    />
    <ChartFrame
      v-if="costByPlace && view === 'costs'"
      class="cost-by-place"
      :hint="t('stats.chart.costByPlace.hint')"
      kind="bar"
      stacked
      :title="t('stats.chart.costByPlace.title')"
      :summary="costByPlace.summary"
      :empty="costByPlace.empty"
      :labels="placeLabels"
      :series="costByPlace.series"
      :table-series="costByPlace.tableSeries"
      :unit="costByPlace.unit"
      :interval-header="t('stats.chart.byPlace.header')"
      :format="costByPlace.format"
      :selectable="false"
    />
  </div>
</template>

<style scoped>
.stats-distributions {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  margin-top: 12px;
}
@media (max-width: 959.98px) {
  .stats-distributions {
    grid-template-columns: minmax(0, 1fr);
    gap: 8px;
    margin-top: 8px;
  }
}
</style>
