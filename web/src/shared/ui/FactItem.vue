<script setup lang="ts">
import TileFigure from './TileFigure.vue'

// FactItem is one figure of an event, in a <dl>: its label and its value, already
// formatted (unknown included), with other words for screen readers if it has them (a
// range of amounts); the slot adds to the value (a link, notes).
defineProps<{ label: string; value: string; spoken?: string }>()
</script>

<template>
  <div class="fact">
    <dt class="label">{{ label }}</dt>
    <dd>
      <TileFigure class="value" size="small" :text="value" :spoken />
      <slot />
    </dd>
  </div>
</template>

<style scoped>
.fact {
  min-width: 0;
}
dd {
  margin: 0;
}
.label {
  margin-bottom: 2px;
  font-size: 0.8125rem;
  color: rgb(var(--v-theme-text-secondary));
}
</style>
