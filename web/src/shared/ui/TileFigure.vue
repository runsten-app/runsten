<script setup lang="ts">
import { computed } from 'vue'
import { figureParts } from '@/shared/lib'
import SpokenText from './SpokenText.vue'

// TileFigure is a value as a tile shows it: light and large, its unit smaller and secondary
// on the same baseline. The text is already formatted, unknown included (then secondary
// as a whole); the two parts together read as the text did. Words used as values
// ("Stopped", "Connected") take the same size.
const props = withDefaults(
  defineProps<{
    text: string
    spoken?: string
    size?: 'hero' | 'large' | 'kpi' | 'small'
    unknown?: boolean
  }>(),
  { spoken: undefined, size: 'large', unknown: false },
)
const parts = computed(() =>
  props.unknown || (props.spoken && props.spoken !== props.text) ? null : figureParts(props.text),
)
</script>

<template>
  <span class="figure" :class="[`figure--${size}`, { 'figure--unknown': unknown }]"
    ><template v-if="parts"
      ><span class="figure-value">{{ parts.value }}</span
      ><span class="figure-unit">{{ parts.unit }}</span></template
    ><SpokenText v-else class="figure-value" :text :spoken
  /></span>
</template>

<style scoped>
.figure {
  display: inline-block;
  white-space: nowrap;
  font-weight: 300;
  font-variant-numeric: tabular-nums;
  line-height: 1.05;
  letter-spacing: -0.02em;
}
.figure-unit {
  font-weight: 400;
  letter-spacing: 0;
  color: rgb(var(--v-theme-text-secondary));
}
.figure--unknown {
  color: rgb(var(--v-theme-text-secondary));
}
.figure--hero {
  font-size: 104px;
  line-height: 0.95;
  letter-spacing: -0.035em;
}
.figure--hero .figure-unit {
  font-size: 28px;
  font-weight: 300;
}
.figure--large {
  font-size: 38px;
}
.figure--large .figure-unit {
  font-size: 14px;
}
.figure--kpi {
  font-size: 36px;
}
.figure--kpi .figure-unit {
  font-size: 13px;
}
.figure--small {
  font-size: 22px;
}
.figure--small .figure-unit {
  font-size: 14px;
}
/* A word, unknown included, wraps rather than overflow its tile. */
.figure--unknown,
.figure:not(:has(.figure-unit)) {
  white-space: normal;
}
@media (max-width: 599.98px) {
  .figure--hero {
    font-size: 80px;
  }
  .figure--hero .figure-unit {
    font-size: 24px;
  }
  .figure--large,
  .figure--kpi {
    font-size: 28px;
  }
}
</style>
