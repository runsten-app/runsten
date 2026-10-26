<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTheme } from 'vuetify'
import { bar, type TyreReading } from '@/entities/health'
import { tyreLine, useTyres } from '@/features/view-health'
import { timeTicks, useFormat, useNow } from '@/shared/lib'
import { ChartFrame, type Series } from '@/shared/ui/chart'

// TyreChart draws the tyres' pressures over a window, as the vehicle gave them: one chart
// per axle, its left and right tyres, and the pressure the vehicle recommends there,
// dashed. A line breaks where nothing was read. The table for screen readers gives each
// day's (each month's over a year) lowest and highest pressure of each tyre.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const f = useFormat()
const theme = useTheme()

type Window = 'week' | 'month' | 'year'
const windows: Window[] = ['week', 'month', 'year']
const windowLabels = computed<Record<Window, string>>(() => ({
  week: t('health.chart.week'),
  month: t('health.chart.month'),
  year: t('health.chart.year'),
}))
const days: Record<Window, number> = { week: 7, month: 30, year: 365 }
const chosen = ref<Window>('month')

// The window moves on every hour: the health is read no more often.
const step = 3600_000
const now = useNow(60_000)
const end = computed(() => Math.floor(now.value / step) * step)
const start = computed(() => end.value - days[chosen.value] * 24 * 3600_000)
const { tyres } = useTyres(
  () => props.vehicle,
  () => ({ from: new Date(start.value).toISOString(), to: new Date(end.value).toISOString() }),
)
const runs = computed(() => tyres.value?.runs ?? [])
const readings = computed(() => runs.value.flatMap((r) => r.readings))

function color(name: string): string {
  const c = theme.current.value.colors[name]
  return typeof c === 'string' ? c : '#000'
}
const inBar = (v: number | null) => (v === null ? null : bar(v))
const format = (v: number | null) => f.quantity(v, 'bar')

type Axle = {
  key: 'front' | 'rear'
  title: string
  left: (r: TyreReading) => number | null
  right: (r: TyreReading) => number | null
  recommended: (r: TyreReading) => number | null
}
const axles = computed<Axle[]>(() => [
  {
    key: 'front',
    title: t('health.chart.front'),
    left: (r) => r.front_left_kpa,
    right: (r) => r.front_right_kpa,
    recommended: (r) => r.recommended_front_kpa,
  },
  {
    key: 'rear',
    title: t('health.chart.rear'),
    left: (r) => r.rear_left_kpa,
    right: (r) => r.rear_right_kpa,
    recommended: (r) => r.recommended_rear_kpa,
  },
])

// The intervals of the table: days, or months over a year, in the reader's time zone.
const byMonth = computed(() => chosen.value === 'year')
const keyOf = (iso: string) => (byMonth.value ? f.dayKey(iso).slice(0, 7) : f.dayKey(iso))
const labelOf = (key: string) => (byMonth.value ? f.calendarMonth(`${key}-01`) : f.calendarDay(key))

function chartOf(a: Axle) {
  const lines: Series[] = [
    {
      label: t('health.chart.left'),
      data: tyreLine(runs.value, (r) => inBar(a.left(r))),
      color: color('chart-1'),
    },
    {
      label: t('health.chart.right'),
      data: tyreLine(runs.value, (r) => inBar(a.right(r))),
      color: color('chart-2'),
    },
    {
      label: t('health.chart.recommended'),
      data: tyreLine(runs.value, (r) => inBar(a.recommended(r))),
      color: color('chart-3'),
      dashed: true,
    },
  ]
  const keys: string[] = []
  for (const r of readings.value) {
    const k = keyOf(r.at)
    if (!keys.includes(k)) keys.push(k)
  }
  const of = (k: string, v: (r: TyreReading) => number | null) =>
    readings.value
      .filter((r) => keyOf(r.at) === k)
      .map(v)
      .filter((x): x is number => x !== null)
      .map(bar)
  const low = (xs: number[]) => (xs.length ? Math.min(...xs) : null)
  const high = (xs: number[]) => (xs.length ? Math.max(...xs) : null)
  const table: Series[] = [
    {
      label: t('health.chart.leftLow'),
      data: keys.map((k) => low(of(k, a.left))),
      color: color('chart-1'),
    },
    {
      label: t('health.chart.leftHigh'),
      data: keys.map((k) => high(of(k, a.left))),
      color: color('chart-1'),
    },
    {
      label: t('health.chart.rightLow'),
      data: keys.map((k) => low(of(k, a.right))),
      color: color('chart-2'),
    },
    {
      label: t('health.chart.rightHigh'),
      data: keys.map((k) => high(of(k, a.right))),
      color: color('chart-2'),
    },
  ]
  const known = (v: (r: TyreReading) => number | null) =>
    readings.value.map(v).filter((x): x is number => x !== null)
  const left = known(a.left)
  const right = known(a.right)
  const summary =
    left.length || right.length
      ? t('health.chart.summary', {
          first: format(inBar(left[0] ?? null)),
          last: format(inBar(left.at(-1) ?? null)),
          firstRight: format(inBar(right[0] ?? null)),
          lastRight: format(inBar(right.at(-1) ?? null)),
        })
      : null
  return { lines, table, labels: keys.map(labelOf), summary }
}
const charts = computed(() => axles.value.map((a) => ({ axle: a, ...chartOf(a) })))

const ticks = computed(() => timeTicks(start.value, end.value, 7).ticks)
const iso = (v: number) => new Date(v).toISOString()
const formatTick = (v: number) => f.bucket(iso(v), byMonth.value ? 'month' : 'day')
const formatPoint = (v: number) => f.dateTime(iso(v), now.value)
</script>

<template>
  <section class="tyre-chart">
    <v-btn-toggle
      v-model="chosen"
      mandatory
      density="compact"
      class="segmented mb-3"
      role="group"
      :aria-label="t('health.chart.window')"
    >
      <v-btn v-for="w in windows" :key="w" :value="w" :aria-pressed="w === chosen">
        {{ windowLabels[w] }}
      </v-btn>
    </v-btn-toggle>
    <template v-if="tyres">
      <ChartFrame
        v-for="c in charts"
        :key="c.axle.key"
        :class="`tyres-${c.axle.key} mb-4`"
        kind="line"
        :title="c.axle.title"
        :summary="c.summary ?? t('health.chart.none')"
        :hint="t('health.chart.hint')"
        :labels="c.labels"
        :series="c.lines"
        :table-series="c.table"
        unit="bar"
        :interval-header="t('health.chart.interval')"
        :format
        :format-x="formatTick"
        :format-point="formatPoint"
        :x-min="start"
        :x-max="end"
        :x-ticks="ticks"
        :begin-at-zero="false"
        :selectable="false"
        :empty="c.summary ? undefined : t('health.chart.none')"
      />
    </template>
  </section>
</template>
