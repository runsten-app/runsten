<script setup lang="ts">
// StatusPill is a state in a word, in a pill: a status's own color over its container,
// with a dot (or an icon, for a neutral state such as parked). The word tells it; the
// color only repeats it.
withDefaults(defineProps<{ tone?: 'success' | 'warning' | 'error' | 'neutral'; icon?: string }>(), {
  tone: 'neutral',
  icon: undefined,
})
</script>

<template>
  <span class="status-pill" :class="`status-pill--${tone}`">
    <v-icon v-if="icon" :icon size="16" aria-hidden="true" />
    <span v-else class="dot" aria-hidden="true" />
    <slot />
  </span>
</template>

<style scoped>
.status-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-block-size: 28px;
  padding: 2px 12px 2px 10px;
  border-radius: 999px;
  font-size: 0.875rem;
  font-weight: 500;
  line-height: 1.2;
}
.dot {
  inline-size: 7px;
  block-size: 7px;
  border-radius: 50%;
  background: currentColor;
}
.status-pill--neutral {
  background: rgb(var(--v-theme-surface));
  color: rgb(var(--v-theme-on-surface));
}
.status-pill--success {
  background: rgb(var(--v-theme-success-container));
  color: rgb(var(--v-theme-success));
}
.status-pill--warning {
  background: rgb(var(--v-theme-warning-container));
  color: rgb(var(--v-theme-warning));
}
.status-pill--error {
  background: rgb(var(--v-theme-error-container));
  color: rgb(var(--v-theme-error));
}
</style>
