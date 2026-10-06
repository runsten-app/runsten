<script setup lang="ts">
import { computed, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTheme } from 'vuetify'
import { useCharges } from '@/features/browse-charges'
import { useTrips } from '@/features/browse-trips'
import { readingsOf, seriesLine, useSeries } from '@/features/view-series'
import { timeTicks, useFormat, useNow, type Bounds } from '@/shared/lib'
import { ChartFrame, type Band, type Series } from '@/shared/ui/chart'

// SocTimeline draws the state of charge of the last 7 days as the vehicle gave it, over
// bands of its trips and charges: between them, it was parked. A line breaks where
// nothing was read; read every hour, the readings show as points. The table for screen
// readers gives each day's lowest and highest state of charge, and its events.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const f = useFormat()
const theme = useTheme()

const days = 7
const step = 5 * 60_000
// The window moves on every five minutes: a key that changed every second would read
// again for nothing, the collector reading no more often than once a minute.
const now = useNow(60_000)
const end = computed(() => Math.floor(now.value / step) * step)
const start = computed(() => end.value - days * 24 * 3600_000)
const from = computed(() => new Date(start.value).toISOString())

// Both ends: without to, the server's now would make the window longer than its 7 days
// by the rounding of the end.
const { series } = useSeries(
  () => props.vehicle,
  () => ({ from: from.value, to: new Date(end.value).toISOString() }),
)
const trips = useTrips(() => props.vehicle, from, undefined)
const charges = useCharges(() => props.vehicle, from, undefined)
// Every event of the week, however many pages: the bands would stop short otherwise.
watchEffect(() => {
  if (trips.hasMore.value && !trips.isLoadingMore.value && !trips.loadMoreFailed.value)
    void trips.loadMore()
  if (charges.hasMore.value && !charges.isLoadingMore.value && !charges.loadMoreFailed.value)
    void charges.loadMore()
})

function color(name: string): string {
  const c = theme.current.value.colors[name]
  return typeof c === 'string' ? c : '#000'
}

const readings = computed(() => readingsOf(series.value?.runs ?? []))
const known = computed(() =>
  readings.value.filter((r): r is typeof r & { soc_pct: number } => r.soc_pct !== null),
)

// A band covers where the event may have been, from its earliest start to its latest
// end, and more strongly where it surely was; a reconstructed one is sure of nothing.
type Event = { start: Bounds; end: Bounds; reconstructed: boolean }
function band(e: Event, label: string, c: string): Band {
  const sureFrom = Date.parse(e.start.before)
  const sureTo = Date.parse(e.end.after)
  return {
    label,
    color: c,
    from: Date.parse(e.start.after),
    to: Date.parse(e.end.before),
    sure: !e.reconstructed && sureTo > sureFrom ? { from: sureFrom, to: sureTo } : undefined,
  }
}
const bands = computed<Band[]>(() => [
  ...trips.trips.value.map((e) => band(e, t('timeline.driving'), color('chart-2'))),
  ...charges.charges.value.map((e) => band(e, t('timeline.charging'), color('chart-3'))),
])

const lines = computed<Series[]>(() => [
  {
    label: t('timeline.soc'),
    data: seriesLine(series.value?.runs ?? [], (r) => r.soc_pct),
    color: color('chart-1'),
  },
])

// The days of the window, in the reader's time zone, each with its lowest and highest
// reading and the events that started in it: the figures behind the chart.
const table = computed(() => {
  const keys: string[] = []
  for (let d = start.value; d <= end.value; d += 3600_000) {
    const k = f.dayKey(new Date(d).toISOString())
    if (!keys.includes(k)) keys.push(k)
  }
  const endKey = f.dayKey(new Date(end.value).toISOString())
  if (!keys.includes(endKey)) keys.push(endKey)
  const ofDay = (k: string) => known.value.filter((r) => f.dayKey(r.at) === k).map((r) => r.soc_pct)
  const started = (events: Event[], k: string) =>
    events.filter((e) => f.dayKey(e.start.after) === k).length
  const count = (v: number | null) => (v === null ? f.unknown() : f.number(v))
  const rows = keys.map((k) => {
    const socs = ofDay(k)
    return {
      label: f.calendarDay(k),
      low: socs.length ? Math.min(...socs) : null,
      high: socs.length ? Math.max(...socs) : null,
      trips: started(trips.trips.value, k),
      charges: started(charges.charges.value, k),
    }
  })
  return {
    labels: rows.map((r) => r.label),
    series: [
      { label: t('timeline.low'), data: rows.map((r) => r.low), color: color('chart-1') },
      { label: t('timeline.high'), data: rows.map((r) => r.high), color: color('chart-1') },
      {
        label: t('timeline.trips'),
        data: rows.map((r) => r.trips),
        color: color('chart-2'),
        format: count,
      },
      {
        label: t('timeline.charges'),
        data: rows.map((r) => r.charges),
        color: color('chart-3'),
        format: count,
      },
    ] satisfies Series[],
  }
})

const summary = computed(() => {
  const r = known.value
  const first = r[0]
  const last = r.at(-1)
  if (!first || !last) return t('timeline.none')
  const socs = r.map((x) => x.soc_pct)
  return t('timeline.summary', {
    first: f.quantity(first.soc_pct, 'percent'),
    last: f.quantity(last.soc_pct, 'percent'),
    min: f.quantity(Math.min(...socs), 'percent'),
    max: f.quantity(Math.max(...socs), 'percent'),
    trips: t('timeline.tripCount', trips.trips.value.length),
    charges: t('timeline.chargeCount', charges.charges.value.length),
  })
})

const ticks = computed(() => timeTicks(start.value, end.value, 7).ticks)
const iso = (v: number) => new Date(v).toISOString()
const formatTick = (v: number) => f.bucket(iso(v), 'day')
const formatPoint = (v: number) => f.time(iso(v))
</script>

<template>
  <ChartFrame
    v-if="series"
    class="soc-timeline"
    kind="line"
    :title="t('timeline.title')"
    :summary
    :hint="t('timeline.hint')"
    :labels="table.labels"
    :series="lines"
    :table-series="table.series"
    :bands
    unit="%"
    :interval-header="t('timeline.day')"
    :format="(v: number | null) => f.quantity(v, 'percent')"
    :format-x="formatTick"
    :format-point="formatPoint"
    :x-min="start"
    :x-max="end"
    :x-ticks="ticks"
    :y-min="0"
    :y-max="100"
    :selectable="false"
    :empty="known.length ? undefined : summary"
  />
</template>
