<script setup lang="ts">
import {
  BarController,
  BarElement,
  CategoryScale,
  Chart,
  Filler,
  LinearScale,
  LineController,
  LineElement,
  PointElement,
  ScatterController,
  Tooltip,
  type ChartData,
  type Plugin,
  type ChartOptions,
} from 'chart.js'
import { computed, ref } from 'vue'
import { Bar, Line } from 'vue-chartjs'
import { useDisplay, useTheme } from 'vuetify'
import type { Band, Series } from './series'

// Only what the charts use: the rest of Chart.js stays out of the bundle.
Chart.register(
  BarController,
  BarElement,
  LineController,
  LineElement,
  ScatterController,
  PointElement,
  CategoryScale,
  LinearScale,
  Filler,
  Tooltip,
  Filler,
)

// ChartFrame is a chart and its data, in a tile. A canvas has no content a screen reader
// reads: it is an image with a summary sentence, and a table only screen readers are
// given holds the same figures. Pressing an interval selects it (the page leads to its
// events). The colors and the typeface are the theme's, light or dark; the legend is
// the page's own (the series and their marks), and the animation stops when the reader
// asks for less motion. Horizontal rules only, ticks never turned: the figures are what
// the eye reads.
const props = withDefaults(
  defineProps<{
    title: string
    summary: string
    kind: 'bar' | 'line'
    labels: string[]
    series: Series[]
    stacked?: boolean
    // The unit of the values, on the axis.
    unit: string
    intervalHeader: string
    format: (v: number | null) => string
    // Says what pressing an interval does.
    hint?: string
    // The figures of the table, when they are not those drawn: a stacked difference (a
    // range's uncertainty) reads better as the range's top.
    tableSeries?: Series[]
    // What the tooltip of an interval tells under its drawn figures, not drawn itself
    // (a band's trips and their speed); the table holds them as tableSeries. Pressing an
    // interval is not needed to read them: the tooltip shows on pointing.
    details?: (index: number) => string[]
    // The x axis is linear, not categories (dates as milliseconds, odometers as
    // kilometers): the ticks are formatX's, and the labels serve the table only.
    formatX?: (v: number) => string
    // The x axis's own ticks, as values (the month starts of the history): a linear
    // axis would place its own anywhere in a month, and the same month twice.
    xTicks?: number[]
    // The x axis never leaves the history: its bounds, as values. Without them the
    // axis would reach for a "nice" number beyond the last month.
    xMin?: number
    xMax?: number
    // The nearest element answers a press (a point of a scatter), not the whole column
    // of an interval.
    nearest?: boolean
    // Whether a press leads anywhere (false): the cursor says so only then. Absent, it
    // does: most charts select an interval or a point.
    selectable?: boolean
    // The y axis begins at zero for sums; a level (an estimated capacity) would read
    // flat on it: false makes it follow what is drawn, between yMin and yMax.
    beginAtZero?: boolean
    yMin?: number
    yMax?: number
    // Said instead of the canvas when there is nothing to draw: the title and the table
    // for screen readers stay, so the chart is never an empty frame.
    empty?: string
    // The tooltip's title over a linear axis, when the ticks say less (a day for a tick,
    // a time for a reading); formatX without it.
    formatPoint?: (v: number) => string
    // Spans of the x axis in a strip under the plot (trips, charges), in the legend after
    // the series.
    bands?: Band[]
    // The second axis, on the right, of the series marked right: its unit and bounds.
    rightUnit?: string
    rightMin?: number
    rightMax?: number
  }>(),
  // The other optional props have no default to give; booleans default to false.
  {
    selectable: true,
    beginAtZero: true,
    hint: undefined,
    tableSeries: undefined,
    details: undefined,
    formatX: undefined,
    xTicks: undefined,
    xMin: undefined,
    xMax: undefined,
    yMin: undefined,
    yMax: undefined,
    empty: undefined,
    formatPoint: undefined,
    bands: undefined,
    rightUnit: undefined,
    rightMin: undefined,
    rightMax: undefined,
  },
)
const emit = defineEmits<{ select: [index: number, dataset: number] }>()
const theme = useTheme()
const display = useDisplay()

// The theme's colors are hex strings once computed; its typeface a CSS font stack.
function token(name: string): string {
  const c = theme.current.value.colors[name]
  return typeof c === 'string' ? c : '#000000'
}
const phone = computed(() => display.xs.value)

// The part of a Chart.js chart the click needs.
type IndexedChart = {
  getElementsAtEventForMode: (
    e: unknown,
    mode: 'index' | 'nearest',
    options: { intersect: boolean },
    useFinalPosition: boolean,
  ) => { index: number; datasetIndex: number }[]
}

const table = computed(() => props.tableSeries ?? props.series)

// The table shows the figure of a series at an interval: the y of a point, unknown
// where there is none.
function tableValue(s: Series, i: number): number | null {
  const v = s.data[i]
  return v === undefined || v === null ? null : typeof v === 'number' ? v : v.y
}

const reducedMotion = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false

// isolated tells a known value between two unknown ones: a line has nothing to join it
// to, so it shows as a point, never not at all.
function isolated(data: Series['data'], i: number): boolean {
  const y = (k: number) => {
    const v = data[k]
    return v === undefined || v === null ? null : typeof v === 'number' ? v : v.y
  }
  return y(i) !== null && y(i - 1) === null && y(i + 1) === null
}

const data = computed(() => ({
  labels: props.labels,
  datasets: props.series.map((s) => {
    // A band fills towards its lower bound; both bounds are thin edges in chart-4. A soft
    // bar (the uncertainty of a cost) fills in the band color, edged in its own.
    const thin = s.band || s.edge
    const line = props.kind === 'line' && !s.scatter
    return {
      label: s.label,
      data: s.data,
      yAxisID: s.right ? 'right' : 'y',
      // A scatter among the lines of the same chart: the chart's kind stays its own.
      type: s.scatter ? ('scatter' as const) : undefined,
      // An area's fill is its own color, faint: the line stays what the eye follows.
      backgroundColor: s.band || s.soft ? token('chart-band') : s.area ? `${s.color}1f` : s.color,
      borderColor: s.color,
      borderWidth: s.soft
        ? 1
        : thin
          ? 1
          : s.dashed
            ? 1.5
            : props.kind === 'bar'
              ? 0
              : s.wide
                ? 6
                : 2.5,
      // Chart.js draws the higher order first, under the others.
      order: s.wide ? 1 : 0,
      borderDash: s.dashed ? [6, 4] : undefined,
      fill: s.band ? ('-1' as const) : s.area ? ('origin' as const) : false,
      // A line breaks where a value is unknown: a gap, never a zero.
      spanGaps: false,
      // A lone known value is a point, a band's bound too: no segment would show it.
      pointRadius: s.scatter
        ? 3
        : line && !s.dashed
          ? (c: { dataIndex: number }) => (isolated(s.data, c.dataIndex) ? 3 : 0)
          : 0,
      pointHoverRadius: thin || s.dashed ? 0 : 4,
      pointBackgroundColor: s.color,
      // A point is worth a press; the column of an interval is wide, a point is not.
      pointHitRadius: s.scatter ? 10 : undefined,
      borderRadius: props.kind === 'bar' ? 2 : undefined,
      categoryPercentage: phone.value ? 0.96 : 0.9,
      barPercentage: 1,
    }
  }),
}))

// legend is the series as the page shows them, each label once: two series of one band
// share their label (an interquartile range is drawn as its two bounds). The spans behind
// them follow.
const legend = computed(() => {
  const seen = new Set<string>()
  const spans = (props.bands ?? [])
    .filter((b) => !seen.has(b.label) && !!seen.add(b.label))
    .map((b) => ({ label: b.label, mark: 'span', color: b.color }))
  seen.clear()
  const series = props.series
    .filter((s) => !seen.has(s.label) && !!seen.add(s.label))
    .map((s) => ({
      label: s.label,
      mark:
        s.band || s.soft
          ? 'band'
          : s.dashed
            ? 'dashed'
            : props.kind === 'line' && !s.scatter
              ? 'line'
              : 'box',
      color: s.color,
    }))
  return [...series, ...spans]
})

// zeros marks an interval measured at zero with a stub on the axis, in the boundary
// color: an unknown interval has no bar at all, and the two must never look alike.
const zeros: Plugin<'bar'> = {
  id: 'runsten-zeros',
  afterDatasetsDraw(chart) {
    const metas = chart.data.datasets.map((_, i) => chart.getDatasetMeta(i))
    const first = metas.find((m) => !m.hidden)
    if (!first) return
    const y = chart.scales.y
    if (!y) return
    const ctx = chart.ctx
    ctx.save()
    ctx.fillStyle = token('boundary')
    first.data.forEach((bar, i) => {
      const values = chart.data.datasets.map((d) => d.data[i])
      if (!values.every((v) => v === 0)) return
      const { x, width } = bar as unknown as { x: number; width: number }
      ctx.fillRect(x - width / 2, y.getPixelForValue(0) - 2, width, 2)
    })
    ctx.restore()
  },
}
// The strip of the bands, under the plot and over the x axis's ticks, in pixels.
const lane = { gap: 6, height: 20 }
// The band under the pointer, and where its tip goes in the canvas, in pixels: from x
// rightwards in the left half, leftwards in the right one, so that it stays inside; up
// from y, over the plot: under the strip, a tip of several lines would leave the card.
type Tip = { band: Band; x: number; y: number; left: boolean }
const tip = ref<Tip | null>(null)
// The part of a Chart.js chart the strip's pointer needs.
type LaneChart = {
  chartArea: { left: number; right: number; bottom: number }
  width: number
  scales: { x?: { getPixelForValue: (v: number) => number } }
}
// bandAt is the band drawn last under the pointer, in the strip: the one seen there. A
// few pixels around a band count, so that one of a pixel can still be pointed at.
function bandAt(chart: LaneChart, px: number, py: number): Tip | null {
  const { chartArea: area, scales } = chart
  const top = area.bottom + lane.gap
  if (!scales.x || !props.bands?.length) return null
  if (py < top || py > top + lane.height || px < area.left - 3 || px > area.right + 3) return null
  const x = scales.x
  const band = props.bands.findLast(
    (b) => px >= x.getPixelForValue(b.from) - 3 && px <= x.getPixelForValue(b.to) + 3,
  )
  if (!band) return null
  return { band, x: px, y: top - 4, left: px > chart.width / 2 }
}
// spans draws the bands in their strip: faint where the event may have been, solid
// where it surely was, named inside its surest part when the name fits.
const spans: Plugin<'line'> = {
  id: 'runsten-spans',
  afterDatasetsDraw(chart) {
    const x = chart.scales.x
    const area = chart.chartArea
    if (!x || !props.bands?.length) return
    const ctx = chart.ctx
    const top = area.bottom + lane.gap
    const span = (from: number, to: number) => {
      const left = Math.max(area.left, x.getPixelForValue(from))
      const right = Math.min(area.right, x.getPixelForValue(to))
      // At least a pixel: a span shorter than one is still there.
      return right >= left ? { left, width: Math.max(1, right - left) } : null
    }
    ctx.save()
    ctx.font = `600 11px ${String(theme.current.value.variables['font-body'] ?? '')}`
    ctx.textAlign = 'center'
    ctx.textBaseline = 'middle'
    for (const b of props.bands) {
      const whole = span(b.from, b.to)
      const sure = b.sure ? span(b.sure.from, b.sure.to) : null
      ctx.fillStyle = b.color
      if (whole) {
        ctx.globalAlpha = 0.4
        ctx.fillRect(whole.left, top, whole.width, lane.height)
      }
      ctx.globalAlpha = 1
      if (sure) ctx.fillRect(sure.left, top, sure.width, lane.height)
      const named = sure ?? whole
      if (named && ctx.measureText(b.label).width + 8 <= named.width) {
        ctx.fillStyle = b.ink ?? token('on-primary')
        ctx.fillText(b.label, named.left + named.width / 2, top + lane.height / 2)
      }
    }
    ctx.restore()
  },
  // Every event, outside the plot too (onHover hears only those inside it): the band
  // under the pointer, none once it leaves. Its tip rises over the plot: the point's
  // tooltip, which the strip's pointer raises too, gives way to it.
  afterEvent(chart, args) {
    const { event } = args
    tip.value =
      event.type === 'mouseout' || event.x === null || event.y === null
        ? null
        : bandAt(chart, event.x, event.y)
    if (tip.value && chart.tooltip?.getActiveElements().length) {
      chart.tooltip.setActiveElements([], { x: 0, y: 0 })
      args.changed = true
    }
  },
}
// The bands read the scales only, the same on a bar chart.
const barPlugins = [zeros, spans as unknown as Plugin<'bar'>]
const linePlugins = [spans]

const options = computed(() => {
  const secondary = token('text-secondary')
  const font = { family: String(theme.current.value.variables['font-body'] ?? ''), size: 11 }
  const ticks = { color: secondary, font, maxRotation: 0 }
  const axis = { ticks, grid: { display: false }, border: { display: false } }
  // The x axis's ticks go under the bands' strip.
  const xTicks = props.bands?.length ? { ...ticks, padding: lane.gap + lane.height + 4 } : ticks
  const inverted = { color: token('background') }
  return {
    responsive: true,
    maintainAspectRatio: false,
    // The whole column of an interval answers, an empty one included: a bar of 0 or a
    // gap of the line is as much an interval as the others. Over a linear axis, a true
    // hit on the nearest point only.
    interaction: props.nearest
      ? ({ mode: 'nearest', intersect: true } as const)
      : ({ mode: 'index', intersect: false } as const),
    onClick: (event: unknown, _: unknown, chart: IndexedChart) => {
      if (props.selectable === false) return
      const [hit] = chart.getElementsAtEventForMode(
        event,
        props.nearest ? 'nearest' : 'index',
        { intersect: props.nearest === true },
        false,
      )
      if (hit) emit('select', hit.index, hit.datasetIndex)
    },
    onHover: (event: { native?: { target?: unknown } }, elements: unknown[]) => {
      if (props.selectable === false) return
      const target = event.native?.target
      if (target instanceof HTMLElement) target.style.cursor = elements.length ? 'pointer' : ''
    },
    animation: reducedMotion ? false : undefined,
    plugins: {
      // The page's own legend, above the canvas.
      legend: { display: false },
      // The text color's fill, the ground's letters: it reads over any series.
      tooltip: {
        backgroundColor: token('on-surface'),
        titleColor: inverted.color,
        bodyColor: inverted.color,
        titleFont: { ...font, size: 12, weight: 600 },
        bodyFont: { ...font, size: 12 },
        cornerRadius: 8,
        padding: 10,
        displayColors: false,
        callbacks: {
          // Over a linear axis the title would be the raw number (milliseconds): the
          // reader's format.
          ...(props.formatX
            ? {
                title: (items: { parsed: { x: number } }[]) =>
                  (props.formatPoint ?? props.formatX)?.(items[0]?.parsed.x ?? 0) ?? '',
              }
            : {}),
          label: (c: { dataset: { label?: string }; datasetIndex: number; raw: unknown }) => {
            const v = typeof c.raw === 'number' ? c.raw : pointY(c.raw)
            const format = props.series[c.datasetIndex]?.format ?? props.format
            return `${c.dataset.label ?? ''}: ${format(v)}`
          },
          afterBody: (items: { dataIndex: number }[]) =>
            props.details && items[0] ? props.details(items[0].dataIndex) : [],
        },
      },
    },
    scales: {
      x: props.formatX
        ? {
            ...axis,
            type: 'linear' as const,
            ...(props.xMin !== undefined ? { min: props.xMin } : {}),
            ...(props.xMax !== undefined ? { max: props.xMax } : {}),
            ticks: { ...xTicks, callback: (v: number) => props.formatX?.(v) ?? String(v) },
            // The widget's ticks (month starts) replace the axis's own: a linear axis
            // graduates anywhere in a month, and the same month twice.
            ...(props.xTicks
              ? {
                  afterBuildTicks: (scale: { ticks: unknown }) => {
                    scale.ticks = props.xTicks?.map((v) => ({ value: v })) ?? []
                  },
                }
              : {}),
          }
        : { ...axis, ticks: xTicks, stacked: props.stacked },
      y: {
        ...axis,
        grid: { color: token('chart-grid') },
        ticks: { ...ticks, maxTicksLimit: 5 },
        stacked: props.stacked,
        beginAtZero: props.beginAtZero,
        ...(props.yMin !== undefined ? { min: props.yMin } : {}),
        ...(props.yMax !== undefined ? { max: props.yMax } : {}),
        title: { display: true, text: props.unit, color: secondary, font },
      },
      ...(props.rightUnit
        ? {
            right: {
              ...axis,
              position: 'right' as const,
              ticks: { ...ticks, maxTicksLimit: 5 },
              beginAtZero: true,
              ...(props.rightMin !== undefined ? { min: props.rightMin } : {}),
              ...(props.rightMax !== undefined ? { max: props.rightMax } : {}),
              title: { display: true, text: props.rightUnit, color: secondary, font },
            },
          }
        : {}),
    },
  }
})

// pointY reads the value of a point given as {x, y} (a linear axis); anything else is
// not a value.
function pointY(raw: unknown): number | null {
  return typeof raw === 'object' && raw !== null && 'y' in raw
    ? ((raw as { y?: unknown }).y as number | null)
    : null
}
</script>

<template>
  <v-card class="chart-frame">
    <!-- actions: a link beside the title (to the page the chart summarizes); the default
         slot: what the chart's tile shows above it (the period's figures). -->
    <v-card-title v-if="$slots.actions" class="d-flex flex-wrap align-center ga-2">
      <h2 class="title me-auto">{{ title }}</h2>
      <slot name="actions" />
    </v-card-title>
    <v-card-title v-else tag="h2">{{ title }}</v-card-title>
    <v-card-text>
      <slot />
      <ul v-if="legend.length > 1 && !empty" class="legend">
        <li v-for="l in legend" :key="l.label">
          <span class="mark" :class="`mark--${l.mark}`" :style="{ '--mark': l.color }" />
          {{ l.label }}
        </li>
      </ul>
      <!-- Nothing to draw: the sentence says why, and the title and the table below stay.
           An empty canvas would pretend the page has something to show. -->
      <p v-if="empty" class="empty text-body-medium mb-0">{{ empty }}</p>
      <div v-else class="canvas">
        <Bar
          v-if="kind === 'bar'"
          :data="data as ChartData<'bar'>"
          :options="options as ChartOptions<'bar'>"
          :plugins="barPlugins"
          :aria-label="summary"
        />
        <Line
          v-else
          :data="data as ChartData<'line'>"
          :options="options as ChartOptions<'line'>"
          :plugins="linePlugins"
          :aria-label="summary"
        />
        <!-- The band under the pointer, for the eye only: the table below says the same. -->
        <div
          v-if="tip"
          class="tip"
          :class="{ 'tip--left': tip.left }"
          aria-hidden="true"
          :style="{ left: `${tip.x}px`, top: `${tip.y}px` }"
        >
          <div class="tip-label">
            <span class="mark" :style="{ '--mark': tip.band.color }" />{{ tip.band.label }}
          </div>
          <div v-for="(line, i) in tip.band.detail" :key="i">{{ line }}</div>
        </div>
      </div>
      <!-- Not a v-card-subtitle: it cuts a long line short on a phone. -->
      <p v-if="hint && !empty" class="hint mt-3 mb-0">{{ hint }}</p>
      <!-- For screen readers only: the figures of the canvas, which they cannot read. A plain
           table: v-table's scrolling wrapper, clipped by d-sr-only, would be a scrollable
           region with nothing to focus (axe's scrollable-region-focusable). -->
      <div class="d-sr-only">
        <table>
          <caption>
            {{
              title
            }}
          </caption>
          <thead>
            <tr>
              <th scope="col">{{ intervalHeader }}</th>
              <th v-for="s in table" :key="s.label" scope="col" class="text-end">
                {{ s.label }}
              </th>
            </tr>
          </thead>
          <tbody>
            <!-- By position: two readings of one minute read alike. -->
            <tr v-for="(label, i) in labels" :key="i">
              <th scope="row">{{ label }}</th>
              <td v-for="s in table" :key="s.label" class="text-end">
                {{ (s.format ?? format)(tableValue(s, i)) }}
              </td>
            </tr>
          </tbody>
        </table>
        <!-- The bands, each with what its tip tells the pointer. -->
        <ul v-if="bands?.some((b) => b.detail)">
          <li v-for="(b, i) in bands" :key="i">{{ [b.label, ...(b.detail ?? [])].join(', ') }}</li>
        </ul>
      </div>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.title {
  font: inherit;
}
.canvas {
  position: relative;
  height: 15rem;
}
.tip {
  position: absolute;
  z-index: 1;
  pointer-events: none;
  padding: 8px 10px;
  border-radius: 8px;
  background: rgb(var(--v-theme-on-surface));
  color: rgb(var(--v-theme-background));
  font-size: 0.75rem;
  line-height: 1.4;
  /* Anchored at the pointer, away from the nearer side: half the canvas is always free. */
  inline-size: max-content;
  max-inline-size: 50%;
  transform: translateY(-100%);
}
.tip--left {
  transform: translate(-100%, -100%);
}
.tip-label {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
}
.hint {
  font-size: 0.75rem;
  line-height: 1.4;
  color: rgb(var(--v-theme-text-secondary));
}
.legend {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 16px;
  margin: 0 0 12px;
  padding: 0;
  list-style: none;
  font-size: 0.75rem;
  color: rgb(var(--v-theme-text-secondary));
}
.legend li {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.mark {
  display: inline-block;
  inline-size: 10px;
  block-size: 10px;
  border-radius: 2px;
  background: var(--mark);
}
.mark--line,
.mark--dashed {
  inline-size: 14px;
  block-size: 0;
  border-radius: 0;
  background: none;
  border-block-start: 2px solid var(--mark);
}
.mark--dashed {
  border-block-start-style: dashed;
}
.mark--band {
  background: rgb(var(--v-theme-chart-band));
  box-shadow: inset 0 0 0 1px var(--mark);
}
</style>
