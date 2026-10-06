<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useTheme } from 'vuetify'
import type {
  CapacityEstimate,
  CapacityMonth,
  CapacityQuartiles,
  RangeAtFull,
} from '@/entities/battery'
import { AxisToggle, useBattery, useCapacityAxis } from '@/features/view-battery'
import { useFormat } from '@/shared/lib'
import { ChartFrame, type Point, type Series } from '@/shared/ui/chart'

// BatteryCharts draws the estimated capacity of each charge against the date or the
// mileage, with the monthly median, its interquartile range and the reference capacity;
// then the displayed range at a full charge, which the vehicle forecasts from its recent
// consumption. Pressing a point of the capacity leads to its charge: the raw data behind
// the estimate.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const f = useFormat()
const theme = useTheme()
const router = useRouter()
const { battery } = useBattery(() => props.vehicle)
const { axis } = useCapacityAxis()

// The theme's colors are hex strings once computed.
function color(name: string): string {
  const c = theme.current.value.colors[name]
  return typeof c === 'string' ? c : '#000'
}

const months = computed(() => battery.value?.months ?? [])
// A month is named by its start, as the statistics name their intervals.
const labels = computed(() => months.value.map((m) => f.bucket(m.start, 'month')))
const monthX = (start: string) => Date.parse(start)
const withCapacity = (m: CapacityMonth): m is CapacityMonth & { capacity: CapacityQuartiles } =>
  !!m.capacity
const withRange = (m: CapacityMonth): m is CapacityMonth & { range_at_full: RangeAtFull } =>
  !!m.range_at_full

// yBounds follows what the chart draws, the reference included, with 5 % of margin,
// rounded to the axis's step and outwards: a level near 75 on an axis from zero reads
// flat, and the fall of the estimate is the point of the page (the statistics' sums
// keep their zero). The holes count for nothing.
function yBounds(
  values: (number | null)[],
  step: number,
): { min: number; max: number } | undefined {
  const known = values.filter((v): v is number => v !== null)
  if (!known.length) return undefined
  const lo = Math.min(...known)
  const hi = Math.max(...known)
  const margin = (hi - lo || hi) * 0.05
  return {
    min: Math.floor((lo - margin) / step) * step,
    max: Math.ceil((hi + margin) / step) * step,
  }
}

// The x axis never leaves the history: from the first month's start to the last one's
// end, never a "nice" number beyond them, and never the years around nothing.
const monthExtent = computed(() => {
  const ms = months.value
  const first = ms[0]
  const last = ms.at(-1)
  if (!first || !last) return undefined
  return { min: monthX(first.start), max: monthX(last.end) }
})

// One label per month at most: a linear axis would graduate anywhere in a month, and
// the same month twice. More months than fit: one in N.
const monthTicks = computed(() => {
  const ms = months.value
  if (!ms.length) return undefined
  const every = Math.max(1, Math.ceil(ms.length / 8))
  return ms.filter((_, i) => i % every === 0).map((m) => monthX(m.start))
})

// plotted is the estimate behind each point of a source's series, in draw order: a
// press's index leads to the charge of the point. By mileage, an estimate without an
// odometer cannot sit on the axis: it is left out, and the summary says so.
function plotted(source: CapacityEstimate['source']) {
  const b = battery.value
  if (!b) return { estimates: [] as CapacityEstimate[], points: [] as Point[] }
  const keep = b.estimates.filter(
    (e) => e.source === source && (axis.value === 'date' || e.odometer_km !== null),
  )
  return {
    estimates: keep,
    points: keep.map((e) => ({
      x: axis.value === 'odometer' ? (e.odometer_km ?? 0) : Date.parse(e.at),
      y: e.capacity_kwh,
    })),
  }
}

const capacity = computed(() => {
  const b = battery.value
  if (!b) return null
  const byDate = axis.value === 'date'
  const power = plotted('power')
  const billed = plotted('billed')
  const series: Series[] = [
    {
      label: t('battery.chart.capacity.power'),
      data: power.points,
      color: color('chart-1'),
      scatter: true,
    },
    {
      label: t('battery.chart.capacity.billed'),
      data: billed.points,
      color: color('chart-2'),
      scatter: true,
    },
  ]
  // The monthly median and its band exist by date only: the mileage axis has no months.
  if (byDate) {
    series.push(
      {
        label: t('battery.chart.capacity.band'),
        data: b.months.map((m) => ({ x: monthX(m.start), y: m.capacity?.q1_kwh ?? null })),
        color: color('chart-4'),
        edge: true,
      },
      {
        // Filled towards the previous series, the first quartile: the band between the
        // two, not a plugin. Both series share their label, so the legend shows the band
        // once; the table for screen readers keeps both quartiles.
        label: t('battery.chart.capacity.band'),
        data: b.months.map((m) => ({ x: monthX(m.start), y: m.capacity?.q3_kwh ?? null })),
        color: color('chart-4'),
        band: true,
      },
      {
        label: t('battery.chart.capacity.median'),
        data: b.months.map((m) => ({ x: monthX(m.start), y: m.capacity?.capacity_kwh ?? null })),
        color: color('chart-1'),
      },
    )
  }
  // The reference spans what is drawn, on either axis: what the estimates are compared
  // with, dashed because it is not data of its own.
  const xs = [...power.points, ...billed.points].map((p) => p.x)
  if (b.reference && xs.length) {
    series.push({
      label: t('battery.chart.capacity.reference'),
      data: [
        { x: Math.min(...xs), y: b.reference.capacity_kwh },
        { x: Math.max(...xs), y: b.reference.capacity_kwh },
      ],
      color: color('chart-3'),
      dashed: true,
    })
  }
  // The table gives the months, not the points one by one: the figures behind the chart.
  const tableSeries: Series[] = [
    {
      label: t('battery.chart.header.median'),
      data: b.months.map((m) => m.capacity?.capacity_kwh ?? null),
      color: color('chart-1'),
    },
    {
      label: t('battery.chart.header.q1'),
      data: b.months.map((m) => m.capacity?.q1_kwh ?? null),
      color: color('chart-4'),
    },
    {
      label: t('battery.chart.header.q3'),
      data: b.months.map((m) => m.capacity?.q3_kwh ?? null),
      color: color('chart-4'),
    },
    {
      label: t('battery.chart.header.estimates'),
      data: b.months.map((m) => m.capacity?.estimates ?? 0),
      color: color('chart-3'),
      format: (v) => (v === null ? f.unknown() : f.number(v)),
    },
  ]
  const summary = byDate ? capacitySummary(b.months) : odometerSummary(b, power, billed)
  // The y axis follows the estimates, the reference and the band: never zero, which
  // would flatten the very fall the page exists to show.
  const drawn = power.points.length + billed.points.length > 0
  const values = [...power.points, ...billed.points].map((p) => p.y)
  if (byDate) {
    for (const m of b.months) {
      if (m.capacity) values.push(m.capacity.capacity_kwh, m.capacity.q1_kwh, m.capacity.q3_kwh)
    }
  }
  if (b.reference && xs.length) values.push(b.reference.capacity_kwh)
  return {
    series,
    tableSeries,
    summary,
    power: power.estimates,
    billed: billed.estimates,
    drawn,
    y: yBounds(values, 1),
  }
})

// capacitySummary is the medians of the first and last month with one, for a screen
// reader: the trend at a glance.
function capacitySummary(ms: CapacityMonth[]): string {
  const drawn = ms.filter(withCapacity)
  const first = drawn[0]
  const last = drawn.at(-1)
  if (first && last && first !== last) {
    return t('battery.chart.capacity.summary', {
      first: f.quantity(first.capacity.capacity_kwh, 'kWh'),
      firstAt: f.bucket(first.start, 'month'),
      last: f.quantity(last.capacity.capacity_kwh, 'kWh'),
      lastAt: f.bucket(last.start, 'month'),
    })
  }
  if (first) {
    return t('battery.chart.capacity.one', {
      value: f.quantity(first.capacity.capacity_kwh, 'kWh'),
      at: f.bucket(first.start, 'month'),
    })
  }
  return t('battery.chart.capacity.none')
}

function odometerSummary(
  b: NonNullable<typeof battery.value>,
  power: { estimates: CapacityEstimate[] },
  billed: { estimates: CapacityEstimate[] },
): string {
  // Left to right on the axis: the billed estimate of one charge may sit before the
  // later power ones, whatever the order the API lists them in.
  const drawn = [...power.estimates, ...billed.estimates].sort(
    (a, e) => (a.odometer_km ?? 0) - (e.odometer_km ?? 0),
  )
  const first = drawn[0]
  const last = drawn.at(-1)
  if (!first) return t('battery.chart.capacity.noOdometer')
  const at = (e: CapacityEstimate) => ({
    value: f.quantity(e.capacity_kwh, 'kWh'),
    km: f.quantity(e.odometer_km, 'km'),
  })
  const firstAt = at(first)
  let summary =
    first === last
      ? t('battery.chart.capacity.odometerOne', firstAt)
      : t('battery.chart.capacity.odometerSummary', {
          first: firstAt.value,
          firstKm: firstAt.km,
          last: at(last ?? first).value,
          lastKm: at(last ?? first).km,
        })
  // The estimates the axis cannot place are left out, never guessed at zero.
  const hidden = b.estimates.length - (power.estimates.length + billed.estimates.length)
  if (hidden > 0) summary = `${summary} ${t('battery.chart.capacity.hidden', hidden)}`
  return summary
}

const rangeAtFull = computed(() => {
  const b = battery.value
  if (!b) return null
  const series: Series[] = [
    {
      label: t('battery.chart.range.series'),
      data: b.months.map((m) => ({ x: monthX(m.start), y: m.range_at_full?.median_km ?? null })),
      color: color('chart-1'),
    },
  ]
  const tableSeries: Series[] = [
    {
      label: t('battery.chart.header.median'),
      data: b.months.map((m) => m.range_at_full?.median_km ?? null),
      color: color('chart-1'),
    },
    {
      label: t('battery.chart.header.trips'),
      data: b.months.map((m) => m.range_at_full?.readings ?? 0),
      color: color('chart-3'),
      format: (v) => (v === null ? f.unknown() : f.number(v)),
    },
  ]
  const drawn = b.months.filter(withRange)
  const first = drawn[0]
  const last = drawn.at(-1)
  let summary = t('battery.chart.range.none')
  if (first && last && first !== last) {
    summary = t('battery.chart.range.summary', {
      first: f.quantity(first.range_at_full.median_km, 'km'),
      firstAt: f.bucket(first.start, 'month'),
      last: f.quantity(last.range_at_full.median_km, 'km'),
      lastAt: f.bucket(last.start, 'month'),
    })
  } else if (first) {
    summary = t('battery.chart.range.one', {
      value: f.quantity(first.range_at_full.median_km, 'km'),
      at: f.bucket(first.start, 'month'),
    })
  }
  return {
    series,
    tableSeries,
    summary,
    drawn: drawn.length > 0,
    y: yBounds(
      drawn.map((m) => m.range_at_full.median_km),
      10,
    ),
  }
})

// The x axis is linear: milliseconds for the dates, kilometers for the mileage. The
// ticks are Intl's, as the rest of the interface: no date adapter, no dependency.
const monthAt = (v: number) => f.bucket(new Date(v).toISOString(), 'month')
const formatX = computed(() =>
  axis.value === 'odometer' ? (v: number) => f.quantity(v, 'km') : monthAt,
)

// A press on a point of the capacity leads to its charge; the lines are not points, and
// the mileage axis leaves out what has no odometer.
function openCharge(dataset: number, index: number) {
  const c = capacity.value
  if (!c) return
  const drawn = dataset === 0 ? c.power : dataset === 1 ? c.billed : []
  const estimate = drawn[index]
  if (!estimate) return
  return router.push({ name: 'charge', params: { vehicle: props.vehicle, id: estimate.charge } })
}
</script>

<template>
  <div v-if="months.length" class="battery-charts d-flex flex-column ga-3">
    <!-- No estimate at all: no axis to pick, the sentence under the title says why. -->
    <div v-if="battery?.estimates.length" class="d-flex justify-end">
      <AxisToggle />
    </div>
    <ChartFrame
      v-if="capacity"
      class="capacity"
      kind="line"
      :hint="t('battery.chart.capacity.hint')"
      :title="t('battery.chart.capacity.title')"
      :summary="capacity.summary"
      :labels
      :series="capacity.series"
      :table-series="capacity.tableSeries"
      unit="kWh"
      :interval-header="t('battery.chart.header.month')"
      :format="(v: number | null) => f.quantity(v, 'kWh')"
      :format-x="formatX"
      :x-min="axis === 'date' ? monthExtent?.min : undefined"
      :x-max="axis === 'date' ? monthExtent?.max : undefined"
      :x-ticks="axis === 'date' ? monthTicks : undefined"
      :y-min="capacity.y?.min"
      :y-max="capacity.y?.max"
      :begin-at-zero="false"
      :empty="capacity.drawn ? undefined : capacity.summary"
      nearest
      @select="(index: number, dataset: number) => openCharge(dataset, index)"
    />
    <!-- The vehicle's own forecast, which follows the season and the driving: shown
         apart, never a measure of the battery. -->
    <ChartFrame
      v-if="rangeAtFull"
      class="range"
      kind="line"
      :hint="t('battery.chart.range.hint')"
      :title="t('battery.chart.range.title')"
      :summary="rangeAtFull.summary"
      :labels
      :series="rangeAtFull.series"
      :table-series="rangeAtFull.tableSeries"
      unit="km"
      :interval-header="t('battery.chart.header.month')"
      :format="(v: number | null) => f.quantity(v, 'km')"
      :format-x="monthAt"
      :x-min="monthExtent?.min"
      :x-max="monthExtent?.max"
      :x-ticks="monthTicks"
      :y-min="rangeAtFull.y?.min"
      :y-max="rangeAtFull.y?.max"
      :begin-at-zero="false"
      :empty="rangeAtFull.drawn ? undefined : rangeAtFull.summary"
      :selectable="false"
    />
  </div>
</template>
