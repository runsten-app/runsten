<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useFormat } from '@/shared/lib'
import { runs, type CapacityTrend } from '../model/trend'

// CapacitySparkline draws the monthly medians of the estimated capacity in the health
// page's battery tile: the line, its middle half as a band behind it, the reference
// dashed. A glance at the trend, as the gauge is a glance at the level: no axis, no
// pointer, no Chart.js (the battery's page has the chart, its points and its table).
// Hand-drawn SVG as the gauge, in the theme's chart colors; the whole is an image whose
// label says the first and last medians, and a table only screen readers are given holds
// each month's figures, as every chart's. A month without a median breaks the line; a
// median alone between two holes is a dot.
const props = defineProps<{ trend: CapacityTrend; label: string; from: string; to: string }>()
const { t } = useI18n()
const f = useFormat()
const kwh = (v: number | null) => f.quantity(v, 'kWh')

// The SVG is stretched to the tile's width: its strokes keep their width
// (non-scaling-stroke), and a dot is a zero-length stroke with round caps.
const width = 100
const height = 48
const x = (i: number) =>
  props.trend.median.length > 1 ? (i / (props.trend.median.length - 1)) * width : 0
const y = (v: number) =>
  height - ((v - props.trend.min) / (props.trend.max - props.trend.min)) * height

const lines = computed(() =>
  runs(props.trend.median).map((r) =>
    r.length === 1
      ? `M ${x(r[0]?.i ?? 0)} ${y(r[0]?.v ?? 0)} h 0`
      : r.map((p, k) => `${k ? 'L' : 'M'} ${x(p.i)} ${y(p.v)}`).join(' '),
  ),
)
// The band where both quartiles are known, one polygon per run, its fill the band's
// color; its edges, the quartiles, thin lines in the uncertainty's color, as the
// battery page draws them.
const bands = computed(() => {
  const { q1, q3 } = props.trend
  const both = q1.map((v, i) => (v === null || q3[i] === null ? null : v))
  return runs(both)
    .filter((r) => r.length > 1)
    .map((r) => {
      const top = r.map((p) => `${x(p.i)} ${y(q3[p.i] ?? p.v)}`)
      const bottom = r.map((p) => `${x(p.i)} ${y(p.v)}`)
      return {
        fill: `M ${[...top, ...[...bottom].reverse()].join(' L ')} Z`,
        q1: `M ${bottom.join(' L ')}`,
        q3: `M ${top.join(' L ')}`,
      }
    })
})
const reference = computed(() => (props.trend.reference === null ? null : y(props.trend.reference)))
</script>

<template>
  <div class="capacity-sparkline">
    <figure role="img" :aria-label="label">
      <svg
        :viewBox="`0 -3 ${width} ${height + 6}`"
        preserveAspectRatio="none"
        aria-hidden="true"
        focusable="false"
      >
        <template v-for="(b, i) in bands" :key="`b${i}`">
          <path class="band" :d="b.fill" />
          <path class="edge" :d="b.q1" />
          <path class="edge" :d="b.q3" />
        </template>
        <line
          v-if="reference !== null"
          class="reference"
          x1="0"
          :x2="width"
          :y1="reference"
          :y2="reference"
        />
        <path v-for="(d, i) in lines" :key="`l${i}`" class="median" :d="d" />
      </svg>
      <div class="ends text-medium-emphasis" aria-hidden="true">
        <span>{{ from }}</span>
        <span>{{ to }}</span>
      </div>
    </figure>
    <table class="d-sr-only">
      <caption>
        {{
          t('battery.chart.capacity.title')
        }}
      </caption>
      <thead>
        <tr>
          <th scope="col">{{ t('battery.chart.header.month') }}</th>
          <th scope="col">{{ t('battery.chart.header.median') }}</th>
          <th scope="col">{{ t('battery.chart.header.q1') }}</th>
          <th scope="col">{{ t('battery.chart.header.q3') }}</th>
          <th v-if="trend.reference !== null" scope="col">
            {{ t('battery.chart.capacity.reference') }}
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(start, i) in trend.starts" :key="start">
          <th scope="row">{{ f.bucket(start, 'month') }}</th>
          <td>{{ kwh(trend.median[i] ?? null) }}</td>
          <td>{{ kwh(trend.q1[i] ?? null) }}</td>
          <td>{{ kwh(trend.q3[i] ?? null) }}</td>
          <td v-if="trend.reference !== null">{{ kwh(trend.reference) }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.capacity-sparkline {
  margin-top: 12px;
}
figure {
  margin: 0;
}
svg {
  display: block;
  width: 100%;
  height: 54px;
  overflow: visible;
}
/* As wide as the page on a desktop: taller, or a fall of a few kWh reads flat. */
@media (min-width: 960px) {
  svg {
    height: 72px;
  }
}
.band {
  fill: rgb(var(--v-theme-chart-band));
}
.edge {
  fill: none;
  stroke: rgb(var(--v-theme-chart-4));
  stroke-width: 1;
  vector-effect: non-scaling-stroke;
}
.reference {
  stroke: rgb(var(--v-theme-chart-3));
  stroke-width: 1;
  stroke-dasharray: 4 4;
  vector-effect: non-scaling-stroke;
}
.median {
  fill: none;
  stroke: rgb(var(--v-theme-chart-1));
  stroke-width: 2.5;
  stroke-linecap: round;
  stroke-linejoin: round;
  vector-effect: non-scaling-stroke;
}
.ends {
  display: flex;
  justify-content: space-between;
  font-size: 0.75rem;
  margin-top: 4px;
}
</style>
