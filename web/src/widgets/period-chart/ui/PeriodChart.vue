<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTheme } from 'vuetify'
import { usePeriod } from '@/features/filter-period'
import { useLimits } from '@/features/view-limits'
import { useStats } from '@/features/view-stats'
import { autoBucket, useFormat, type Days } from '@/shared/lib'
import { ChartFrame, type Series } from '@/shared/ui/chart'

// PeriodChart is the list's period at a glance: the distance of the trips, or the energy
// of the charges, by day, week or month as its length says (months for a period open at
// the start). Pressing a bar narrows the list to its interval. The analysis stays on the
// statistics page: one chart here, compact, never more.
const props = defineProps<{
  vehicle: string
  from?: string
  to?: string
  days: Days
  kind: 'trips' | 'charges'
}>()
const { t } = useI18n()
const f = useFormat()
const theme = useTheme()
const { setPeriod } = usePeriod()
const { lacks } = useLimits()
const bucket = computed(() => autoBucket(props.days, new Date()))
// Where the offer leaves out the statistics by interval, none is asked: the totals'
// query, the list's own, and no chart.
const { stats } = useStats(
  () => props.vehicle,
  () => props.from,
  () => props.to,
  () => (lacks('stats') ? undefined : bucket.value),
)

function color(name: string): string {
  const c = theme.current.value.colors[name]
  return typeof c === 'string' ? c : '#000'
}

const intervals = computed(() => stats.value?.buckets ?? [])
const labels = computed(() => intervals.value.map((b) => f.bucket(b.start, bucket.value)))
const intervalHeader = computed(
  () =>
    ({
      day: t('stats.bucket.dayHeader'),
      week: t('stats.bucket.weekHeader'),
      month: t('stats.bucket.monthHeader'),
    })[bucket.value],
)
const count = computed(() =>
  props.kind === 'trips'
    ? (stats.value?.totals.trips.count ?? 0)
    : (stats.value?.totals.charges.count ?? 0),
)

const chart = computed(() => {
  if (props.kind === 'trips') {
    const data = intervals.value.map((b) => b.trips.distance_km)
    const total = stats.value?.totals.trips.distance_km ?? 0
    const top = Math.max(0, ...data.map((v) => v ?? 0))
    const at = labels.value[data.findIndex((v) => v === top)] ?? ''
    return {
      title: t('stats.chart.distance.title'),
      unit: 'km',
      stacked: false,
      format: (v: number | null) => f.quantity(v, 'km'),
      series: [
        { label: t('stats.chart.distance.series'), data, color: color('chart-1') },
      ] as Series[],
      summary: t('stats.chart.distance.summary', {
        total: f.quantity(total, 'km'),
        max: f.quantity(top, 'km'),
        at,
      }),
    }
  }
  const by = (k: 'ac' | 'dc' | 'unknown') =>
    intervals.value.map((b) => b.charges.by_type[k].energy_soc_kwh)
  const types = stats.value?.totals.charges.by_type
  const series: Series[] = [
    { label: t('stats.chart.energy.ac'), data: by('ac'), color: color('chart-1') },
    { label: t('stats.chart.energy.dc'), data: by('dc'), color: color('chart-2') },
  ]
  // Reconstructed charges have no type: a third series, so that the bars add up.
  if (types?.unknown.count)
    series.push({
      label: t('stats.chart.energy.unknown'),
      data: by('unknown'),
      color: color('chart-3'),
    })
  return {
    title: t('stats.chart.energy.title'),
    unit: 'kWh',
    stacked: true,
    format: (v: number | null) => f.quantity(v, 'kWh', 0),
    series,
    summary: t('stats.chart.energy.summary', {
      ac: f.quantity(types?.ac.energy_soc_kwh ?? 0, 'kWh', 0),
      dc: f.quantity(types?.dc.energy_soc_kwh ?? 0, 'kWh', 0),
    }),
  }
})

// narrow keeps the interval's days in the period, as the filter keeps them: the last one
// the day before its end.
function narrow(index: number) {
  const b = intervals.value[index]
  if (!b) return
  const lastDay = new Date(Date.parse(b.end) - 1).toISOString()
  return setPeriod({ from: f.dayKey(b.start), to: f.dayKey(lastDay) })
}
</script>

<template>
  <!-- Nothing in the period: the list says so, an empty chart would not. -->
  <ChartFrame
    v-if="count > 0 && intervals.length > 1"
    class="period-chart mb-4"
    :class="kind"
    kind="bar"
    :stacked="chart.stacked"
    :title="chart.title"
    :summary="chart.summary"
    :hint="t('periodChart.hint')"
    :labels
    :series="chart.series"
    :unit="chart.unit"
    :interval-header="intervalHeader"
    :format="chart.format"
    @select="(i: number) => narrow(i)"
  />
</template>

<style scoped>
.period-chart :deep(.canvas) {
  height: 9rem;
}
</style>
