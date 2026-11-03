<script setup lang="ts">
import { computed, ref, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTheme } from 'vuetify'
import type { Charge } from '@/entities/charge'
import type { Trip } from '@/entities/trip'
import { useCharges } from '@/features/browse-charges'
import { useTrips } from '@/features/browse-trips'
import { useSoftwareVersions } from '@/features/view-health'
import { readingsOf, seriesLine, useSeries } from '@/features/view-series'
import { timeTicks, useFormat, useNow, type Bounds } from '@/shared/lib'
import { ChartFrame, type Band, type Series } from '@/shared/ui/chart'

// SocTimeline draws the state of charge of the last 24 hours, or 7 days, as the vehicle
// gave it, and under it a strip of what the vehicle was doing: driving, charging,
// installing an update, or parked between them while it was read, asleep where its
// source told so; empty where nothing was. A line breaks there too; read every hour, the
// readings show as points. The pointer over a trip or a charge tells its figures, as the
// lists do. The table for screen readers gives each day's lowest and highest state of
// charge, and its events.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const f = useFormat()
const theme = useTheme()

// The last day by default: a trip of half an hour is a sliver in a week.
type Window = 'day' | 'week'
const windows: Window[] = ['day', 'week']
const shown = ref<Window>('day')
const days = computed(() => (shown.value === 'day' ? 1 : 7))
// Each window's words, written out: the linter finds a key only when it is whole.
const words = computed(() =>
  shown.value === 'day'
    ? { over: t('timeline.over.day'), none: t('timeline.none.day') }
    : { over: t('timeline.over.week'), none: t('timeline.none.week') },
)
const windowLabels = computed<Record<Window, string>>(() => ({
  day: t('timeline.window.day'),
  week: t('timeline.window.week'),
}))
const step = 5 * 60_000
// The window moves on every five minutes: a key that changed every second would read
// again for nothing, the collector reading no more often than once a minute.
const now = useNow(60_000)
const end = computed(() => Math.floor(now.value / step) * step)
const start = computed(() => end.value - days.value * 24 * 3600_000)
const from = computed(() => new Date(start.value).toISOString())

// Both ends: without to, the server's now would make the window longer than its days by
// the rounding of the end.
const { series } = useSeries(
  () => props.vehicle,
  () => ({ from: from.value, to: new Date(end.value).toISOString() }),
)
const trips = useTrips(() => props.vehicle, from, undefined)
const charges = useCharges(() => props.vehicle, from, undefined)
// The newest versions are enough: a vehicle takes fewer than one a week. Read with the
// software's tile, from the same cache.
const software = useSoftwareVersions(() => props.vehicle)
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
// end, and solidly where it surely was; a reconstructed one is sure of nothing.
type Event = { start: Bounds; end: Bounds; reconstructed: boolean }
// What the pointer over a band tells first: the day it began, its times (the end's day only
// when another), how long it lasted; as honest as its bounds. A reconstructed event, as
// in the lists, is the one interval it lies within, with no duration.
function detail(start: Bounds, end: Bounds, reconstructed = false): string[] {
  const day = f.dayKey(start.after)
  if (reconstructed)
    return [
      f.calendarDay(day),
      t('event.between', { from: f.time(start.after, day), to: f.time(end.before, day) }),
    ]
  return [
    f.calendarDay(day),
    `${f.bounds(start, day)} – ${f.bounds(end, day)}`,
    f.duration(start, end),
  ]
}
function band(e: Event, label: string, c: string, figures: string[]): Band {
  const sureFrom = Date.parse(e.start.before)
  const sureTo = Date.parse(e.end.after)
  return {
    label,
    color: c,
    from: Date.parse(e.start.after),
    to: Date.parse(e.end.before),
    sure: !e.reconstructed && sureTo > sureFrom ? { from: sureFrom, to: sureTo } : undefined,
    detail: [...detail(e.start, e.end, e.reconstructed), ...figures],
  }
}
// Then what the lists tell of it, each figure only when known: a trip's ends, its
// distance, the state of charge it used and its consumption…
function tripFigures(trip: Trip): string[] {
  const out: string[] = []
  const from = trip.start_place?.name ?? trip.start_address
  const to = trip.end_place?.name ?? trip.end_address
  if (from || to)
    out.push(t('trip.route', { from: from ?? t('trip.elsewhere'), to: to ?? t('trip.elsewhere') }))
  const { distance_km: d, energy_kwh: e } = trip
  const figures: string[] = []
  if (d !== null) figures.push(f.quantity(d, 'km'))
  if (trip.start_soc_pct !== null || trip.end_soc_pct !== null)
    figures.push(f.change(trip.start_soc_pct, trip.end_soc_pct, 'percent'))
  // As the statistics count it: its energy over a positive distance.
  if (e !== null && d !== null && d > 0) figures.push(f.quantity((e * 100) / d, 'kWhPer100km'))
  if (figures.length) out.push(figures.join(' · '))
  return out
}
// …a charge's place, its type, the state of charge it went through, the energy it added
// (measured when the vehicle did, else from the state of charge), its average power and
// its cost.
function chargeFigures(charge: Charge): string[] {
  const out: string[] = []
  const where = charge.place?.name ?? charge.address
  if (where) out.push(where)
  const figures: string[] = []
  if (charge.type) figures.push(charge.type)
  if (charge.start_soc_pct !== null || charge.end_soc_pct !== null)
    figures.push(f.change(charge.start_soc_pct, charge.end_soc_pct, 'percent'))
  const energy = charge.energy_measured_kwh ?? charge.energy_soc_kwh
  if (energy !== null) figures.push(f.quantity(energy, 'kWh'))
  if (figures.length) out.push(figures.join(' · '))
  if (charge.average_power_kw !== null)
    out.push(t('timeline.averagePower', { power: f.quantity(charge.average_power_kw, 'kW') }))
  if (charge.cost) out.push(f.chargeCost(charge.cost).text)
  return out
}
// An update's installation, as far as it was seen: from its start, which a reading saw,
// to the first reading of the version; none when no reading saw it installing. Only
// those within the window: the older versions would be listed for screen readers.
const updates = computed<Band[]>(() =>
  software.versions.value.flatMap((v) => {
    if (!v.install_start) return []
    const installed = {
      after: v.installed.after ?? v.install_start.before,
      before: v.installed.before,
    }
    const b = band(
      { start: v.install_start, end: installed, reconstructed: false },
      t('timeline.update'),
      color('warning'),
      [t('timeline.version', { version: v.version })],
    )
    return b.to > start.value && b.from < end.value ? [b] : []
  }),
)
const events = computed<Band[]>(() => [
  ...trips.trips.value.map((e) => band(e, t('timeline.driving'), color('chart-2'), tripFigures(e))),
  ...charges.charges.value.map((e) =>
    band(e, t('timeline.charging'), color('chart-5'), chargeFigures(e)),
  ),
])
// Parked is wherever the vehicle was read, from the first reading of a run to its last,
// or told awake, outside its trips, charges and updates (as far as they may reach); asleep (or
// offline) wherever it was told so, outside them too, and parked no more. A vehicle
// asleep is not read: the stretches asleep fall between the runs. Each stretch is its
// own band, named where it has room; where nothing was read, no band. Until every page
// of the events is read (the first still coming, one failed, more to come), a trip may
// hide in them: they are then only maybe, paler.
type Span = [number, number]
// minus is the parts of the sorted spans outside the sorted cuts.
function minus(spans: Span[], cuts: Span[]): Span[] {
  return spans.flatMap(([from, end]) => {
    const out: Span[] = []
    let at = from
    for (const [a, b] of cuts) {
      if (b <= at || a >= end) continue
      if (a > at) out.push([at, a])
      at = Math.max(at, b)
    }
    if (end > at) out.push([at, end])
    return out
  })
}
// union joins the spans that overlap or touch, sorted.
function union(spans: Span[]): Span[] {
  const out: Span[] = []
  for (const [a, b] of [...spans].sort((x, y) => x[0] - y[0])) {
    const last = out.at(-1)
    if (last && a <= last[1]) last[1] = Math.max(last[1], b)
    else out.push([a, b])
  }
  return out
}
const sleep = computed(() =>
  (series.value?.sleep ?? []).map((s) => ({
    span: [Date.parse(s.from), Date.parse(s.to)] as Span,
    state: s.state,
  })),
)
const strip = computed<Band[]>(() => {
  // The versions' later pages are older than the window: none needed.
  const complete =
    [trips, charges].every((e) => !e.isPending.value && !e.error.value && !e.hasMore.value) &&
    !software.isPending.value &&
    !software.error.value
  const busy = union([...events.value, ...updates.value].map((e): Span => [e.from, e.to]))
  const at = (v: number) => {
    const iso = new Date(v).toISOString()
    return { after: iso, before: iso }
  }
  const stretch =
    (label: string, c: string, ink: string, extra: string[]) =>
    ([from, to]: Span) => ({
      label,
      color: c,
      ink,
      from,
      to,
      sure: complete ? { from, to } : undefined,
      detail: [...detail(at(from), at(to)), ...extra],
    })
  const asleep = sleep.value
    .filter((s) => s.state !== 'awake')
    .flatMap((s) =>
      minus([s.span], busy).map(
        stretch(
          t('timeline.asleep'),
          color('chart-3'),
          color('on-primary'),
          s.state === 'offline' ? [t('timeline.offline')] : [],
        ),
      ),
    )
  const read = union([
    ...(series.value?.runs ?? []).flatMap((run): Span[] => {
      const first = run.readings[0]
      const last = run.readings.at(-1)
      return first && last ? [[Date.parse(first.at), Date.parse(last.at)]] : []
    }),
    ...sleep.value.filter((s) => s.state === 'awake').map((s) => s.span),
  ])
  const parked = minus(read, union([...busy, ...asleep.map((b): Span => [b.from, b.to])])).map(
    stretch(t('timeline.parked'), color('track'), color('text-secondary'), []),
  )
  return [...parked, ...asleep]
})
const bands = computed<Band[]>(() => [...strip.value, ...events.value, ...updates.value])

const lines = computed<Series[]>(() => [
  {
    label: t('timeline.soc'),
    data: seriesLine(series.value?.runs ?? [], (r) => r.soc_pct),
    color: color('chart-1'),
    area: true,
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
        color: color('chart-5'),
        format: count,
      },
    ] satisfies Series[],
  }
})

const summary = computed(() => {
  const r = known.value
  const first = r[0]
  const last = r.at(-1)
  if (!first || !last) return words.value.none
  const socs = r.map((x) => x.soc_pct)
  return t('timeline.summary', {
    over: words.value.over,
    first: f.quantity(first.soc_pct, 'percent'),
    last: f.quantity(last.soc_pct, 'percent'),
    min: f.quantity(Math.min(...socs), 'percent'),
    max: f.quantity(Math.max(...socs), 'percent'),
    trips: t('timeline.tripCount', trips.trips.value.length),
    charges: t('timeline.chargeCount', charges.charges.value.length),
  })
})

const ticks = computed(() => timeTicks(start.value, end.value, 7))
const iso = (v: number) => new Date(v).toISOString()
// A day for a tick over the week, an hour alone over the day (its own day given, the time
// leaves it out).
function formatTick(v: number): string {
  const at = iso(v)
  return ticks.value.daily ? f.bucket(at, 'day') : f.time(at, f.dayKey(at))
}
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
    :x-ticks="ticks.ticks"
    :y-min="0"
    :y-max="100"
    :selectable="false"
    :empty="known.length ? undefined : summary"
  >
    <template #actions>
      <!-- Only this tile changes: the choice stays out of the URL. -->
      <v-btn-toggle
        v-model="shown"
        mandatory
        density="compact"
        class="segmented"
        role="group"
        :aria-label="t('timeline.window.label')"
      >
        <v-btn v-for="w in windows" :key="w" :value="w" :aria-pressed="w === shown">
          {{ windowLabels[w] }}
        </v-btn>
      </v-btn-toggle>
    </template>
  </ChartFrame>
</template>

<style scoped>
/* Beside the vehicle's tile, the chart takes the row's height: its canvas grows rather
   than leave a blank band under the hint. */
.soc-timeline {
  display: flex;
  flex-direction: column;
}
.soc-timeline :deep(.v-card-text) {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
}
.soc-timeline :deep(.canvas) {
  flex: 1 1 auto;
  min-height: 15rem;
}
</style>
