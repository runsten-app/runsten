<script setup lang="ts">
import { computed } from 'vue'
import { runs, type CapacityTrend } from '../model/trend'

// CapacitySparkline draws the monthly medians of the estimated capacity in the health
// page's battery tile: the line, its middle half as a band behind it, the reference
// dashed. A glance at the trend, as the gauge is a glance at the level: no axis, no
// pointer, no Chart.js (the battery's page has the chart, its points and its table).
// Hand-drawn SVG as the gauge, in the theme's chart colors; the whole is an image whose
// label says the first and last medians. A month without a median breaks the line; a
// median alone between two holes is a dot.
const props = defineProps<{ trend: CapacityTrend; label: string; from: string; to: string }>()

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
// The band where both quartiles are known, as one polygon per run.
const bands = computed(() => {
  const { q1, q3 } = props.trend
  const both = q1.map((v, i) => (v === null || q3[i] === null ? null : v))
  return runs(both)
    .filter((r) => r.length > 1)
    .map((r) => {
      const top = r.map((p) => `${x(p.i)} ${y(q3[p.i] ?? p.v)}`)
      const bottom = r.map((p) => `${x(p.i)} ${y(p.v)}`).reverse()
      return `M ${[...top, ...bottom].join(' L ')} Z`
    })
})
const reference = computed(() => (props.trend.reference === null ? null : y(props.trend.reference)))
</script>

<template>
  <figure class="capacity-sparkline" role="img" :aria-label="label">
    <svg
      :viewBox="`0 -3 ${width} ${height + 6}`"
      preserveAspectRatio="none"
      aria-hidden="true"
      focusable="false"
    >
      <path v-for="(d, i) in bands" :key="`b${i}`" class="band" :d="d" />
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
</template>

<style scoped>
.capacity-sparkline {
  margin: 12px 0 0;
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
