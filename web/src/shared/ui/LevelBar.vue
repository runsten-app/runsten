<script setup lang="ts">
import { computed } from 'vue'

// LevelBar is a level from 0 to 100 (a state of charge) as a thin bar, the accent over
// the track, with a tick at a target when there is one. The figure is written beside it:
// the bar is a meter for screen readers, never the only telling of the value. Shown, it
// is drawn at its value, and glides to a new one; while flowing (a charge running), a
// light runs twice along the fill, again at each new value: a few seconds, so that
// nothing moves on without end (WCAG 2.2.2).
const props = defineProps<{
  value: number
  target?: number | null
  label: string
  flowing?: boolean
}>()
const clamp = (v: number) => Math.min(100, Math.max(0, v))
// The width through a variable, which the transition follows.
const fill = computed(() => ({ '--level': `${clamp(props.value)}%` }))
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
    <div class="fill" :style="fill">
      <span v-if="flowing" :key="value" class="flow" />
    </div>
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
  position: relative;
  overflow: hidden;
  inline-size: var(--level);
  block-size: 100%;
  border-radius: 4px;
  background: rgb(var(--v-theme-primary));
}
@keyframes flow {
  from {
    translate: -100% 0;
  }
  to {
    translate: 250% 0;
  }
}
@media (prefers-reduced-motion: no-preference) {
  .fill {
    transition: inline-size 1s var(--motion-spring);
  }
  .flow {
    position: absolute;
    inset-block: 0;
    inline-size: 40%;
    background: linear-gradient(
      90deg,
      transparent,
      rgb(var(--v-theme-on-primary) / 0.45),
      transparent
    );
    animation: flow 1.8s ease-in-out 2 both;
  }
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
