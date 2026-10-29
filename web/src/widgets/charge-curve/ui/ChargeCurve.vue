<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTheme } from 'vuetify'
import type { ChargeInProgress } from '@/entities/charge'
import { useCharge } from '@/features/browse-charges'
import { readingsOf, seriesLine, useSeries } from '@/features/view-series'
import { kilowatts, timeTicks, useFormat } from '@/shared/lib'
import { ChartFrame, type Series } from '@/shared/ui/chart'

// ChargeCurve draws a charge's power and state of charge against time, as the vehicle
// gave them between its bounds. A curve needs readings close together, as a charge read
// every minute gives: with fewer than minReadings of the power, the chart says how many
// there are rather than draw a line between a few far apart. A reconstructed charge was
// never seen, and has none. A charge in progress (live) is drawn up to its latest
// reading, and grows as it is read again.
const props = defineProps<{ vehicle: string; id: string; live?: ChargeInProgress | null }>()
const { t } = useI18n()
const f = useFormat()
const theme = useTheme()
const minReadings = 5
// The API's window, 7 days at most: a longer charge is drawn over its first week.
const maxWindow = 7 * 24 * 3600_000

const { charge: derived } = useCharge(
  () => props.vehicle,
  () => props.id,
  () => !props.live,
)
// The bounds of the window: a charge's, or one in progress up to its latest reading.
const span = computed(() => {
  const l = props.live
  if (l) return { start: l.start.after, end: l.read_at }
  const c = derived.value
  return c && !c.reconstructed ? { start: c.start.after, end: c.end.before } : undefined
})
const window = computed(() => {
  const s = span.value
  if (!s) return undefined
  const from = Date.parse(s.start)
  const to = Math.min(Date.parse(s.end), from + maxWindow)
  return { from: s.start, to: new Date(to).toISOString() }
})
const { series } = useSeries(() => props.vehicle, window)

function color(name: string): string {
  const c = theme.current.value.colors[name]
  return typeof c === 'string' ? c : '#000'
}

const runs = computed(() => series.value?.runs ?? [])
const readings = computed(() => readingsOf(runs.value))
const withPower = computed(() => readings.value.filter((r) => r.power_w !== null))
const drawn = computed(() => withPower.value.length >= minReadings)

const kw = (v: number | null) => f.quantity(v, 'kW')
const pct = (v: number | null) => f.quantity(v, 'percent')
const lines = computed<Series[]>(() => [
  {
    label: t('chargeCurve.power'),
    data: seriesLine(runs.value, (r) => (r.power_w === null ? null : kilowatts(r.power_w))),
    color: color('chart-1'),
    format: kw,
  },
  {
    label: t('chargeCurve.soc'),
    data: seriesLine(runs.value, (r) => r.soc_pct),
    color: color('chart-2'),
    format: pct,
    right: true,
  },
])

// The table gives every reading: the raw data behind the curve.
const iso = (v: number) => new Date(v).toISOString()
const day = computed(() => (span.value ? f.dayKey(span.value.start) : undefined))
const table = computed(() => ({
  labels: readings.value.map((r) => f.time(r.at, day.value)),
  series: [
    {
      label: t('chargeCurve.power'),
      data: readings.value.map((r) => (r.power_w === null ? null : kilowatts(r.power_w))),
      color: color('chart-1'),
      format: kw,
    },
    {
      label: t('chargeCurve.soc'),
      data: readings.value.map((r) => r.soc_pct),
      color: color('chart-2'),
      format: pct,
    },
  ] satisfies Series[],
}))

const summary = computed(() => {
  if (!drawn.value) return t('chargeCurve.few', withPower.value.length)
  const peak = Math.max(...withPower.value.map((r) => r.power_w ?? 0))
  const socs = readings.value.map((r) => r.soc_pct).filter((v): v is number => v !== null)
  return t('chargeCurve.summary', {
    peak: kw(kilowatts(peak)),
    from: pct(socs[0] ?? null),
    to: pct(socs.at(-1) ?? null),
  })
})

const extent = computed(() => {
  const w = window.value
  return w ? { min: Date.parse(w.from), max: Date.parse(w.to) } : undefined
})
const ticks = computed(() =>
  extent.value ? timeTicks(extent.value.min, extent.value.max, 6).ticks : undefined,
)
const formatTick = (v: number) => f.time(iso(v), f.dayKey(iso(v)))
const formatPoint = (v: number) => f.time(iso(v), day.value)
</script>

<template>
  <ChartFrame
    v-if="window && series"
    class="charge-curve"
    kind="line"
    :title="t('chargeCurve.title')"
    :summary
    :hint="t('chargeCurve.hint')"
    :labels="table.labels"
    :series="lines"
    :table-series="table.series"
    unit="kW"
    right-unit="%"
    :right-min="0"
    :right-max="100"
    :interval-header="t('chargeCurve.time')"
    :format="kw"
    :format-x="formatTick"
    :format-point="formatPoint"
    :x-min="extent?.min"
    :x-max="extent?.max"
    :x-ticks="ticks"
    :selectable="false"
    :empty="drawn ? undefined : summary"
  />
</template>
