<script setup lang="ts">
import { computed } from 'vue'

// LevelBar is a level from 0 to 100 (a state of charge) as a thin bar, the accent over
// the track, with a tick at a target when there is one. The figure is written beside it:
// the bar is a meter for screen readers, never the only telling of the value.
const props = defineProps<{ value: number; target?: number | null; label: string }>()
const clamp = (v: number) => Math.min(100, Math.max(0, v))
const fill = computed(() => ({ inlineSize: `${clamp(props.value)}%` }))
const tick = computed(() =>
  props.target === null || props.target === undefined
    ? null
    : { insetInlineStart: `${clamp(props.target)}%` },
)
</script>

<template>
  <div
    class="level-bar"
    role="meter"
    :aria-label="label"
    aria-valuemin="0"
    aria-valuemax="100"
    :aria-valuenow="clamp(value)"
  >
    <div class="fill" :style="fill" />
    <div v-if="tick" class="target" :style="tick" />
  </div>
</template>

<style scoped>
.level-bar {
  position: relative;
  block-size: 8px;
  border-radius: 4px;
  background: rgb(var(--v-theme-track));
}
.fill {
  block-size: 100%;
  border-radius: 4px;
  background: rgb(var(--v-theme-primary));
}
.target {
  position: absolute;
  inset-block-start: -4px;
  inline-size: 2px;
  block-size: 16px;
  margin-inline-start: -1px;
  background: rgb(var(--v-theme-on-surface));
}
</style>
