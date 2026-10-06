<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useFormat } from '@/shared/lib'
import { arcShare, GAUGE_MAX, GAUGE_MIN, type Remaining } from '../model/remaining'

// CapacityGauge draws the remaining capacity on a ring open at the bottom: the accent up
// to the low end of its margin (the middle half of the estimates), then the margin in the
// band color up to its high end. An estimate, not a reading: the margin is always drawn
// with it. The ring is decoration for the eye; a screen reader reads the meter's value
// text, which says the exact figure and its margin. Without an estimate the track alone
// stays, the slot (the figure, or why there is none) in its middle.
const props = defineProps<{ remaining: Remaining | null; label: string }>()
const { t } = useI18n()
const f = useFormat()

// 240° from bottom left to bottom right, around (100, 95).
const arc = 'M 30.72 135 A 80 80 0 1 1 169.28 135'
const pct = (v: number) => f.quantity(v, 'percent')
const fill = computed(() => {
  const r = props.remaining
  return r ? `${arcShare(r.low)} 100` : null
})
// A dash of the margin's length, after a gap up to its low end.
const band = computed(() => {
  const r = props.remaining
  if (!r) return null
  const low = arcShare(r.low)
  const high = arcShare(r.high)
  return `0 ${low} ${Math.max(high - low, 0.5)} 100`
})
const valueText = computed(() => {
  const r = props.remaining
  return r
    ? t('battery.tile.gaugeText', { pct: pct(r.pct), low: pct(r.low), high: pct(r.high) })
    : ''
})
</script>

<template>
  <div
    class="capacity-gauge"
    v-bind="
      remaining
        ? {
            role: 'meter',
            'aria-label': label,
            'aria-valuemin': 0,
            'aria-valuemax': 100,
            'aria-valuenow': Math.min(100, Math.max(0, remaining.pct)),
            'aria-valuetext': valueText,
          }
        : {}
    "
  >
    <svg viewBox="0 0 200 165" aria-hidden="true" focusable="false">
      <path class="track" :d="arc" pathLength="100" />
      <path v-if="band" class="band" :d="arc" pathLength="100" :stroke-dasharray="band" />
      <path
        v-if="fill && remaining && arcShare(remaining.low) > 0"
        class="fill"
        :d="arc"
        pathLength="100"
        :stroke-dasharray="fill"
      />
      <text x="30.72" y="160" text-anchor="middle">{{ pct(GAUGE_MIN) }}</text>
      <text x="169.28" y="160" text-anchor="middle">{{ pct(GAUGE_MAX) }}</text>
    </svg>
    <div class="centre">
      <slot />
    </div>
  </div>
</template>

<style scoped>
.capacity-gauge {
  position: relative;
  inline-size: 220px;
  max-inline-size: 100%;
}
svg {
  display: block;
  inline-size: 100%;
  fill: none;
  stroke-width: 14;
}
.track {
  stroke: rgb(var(--v-theme-track));
  stroke-linecap: round;
}
.band {
  stroke: rgb(var(--v-theme-chart-band));
}
.fill {
  stroke: rgb(var(--v-theme-primary));
  stroke-linecap: round;
}
text {
  fill: rgb(var(--v-theme-text-secondary));
  stroke: none;
  font-size: 11px;
}
.centre {
  position: absolute;
  inset: 26% 18% auto;
  text-align: center;
}
</style>
