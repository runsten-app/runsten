<script setup lang="ts">
// StatusPill is a state in a word, in a pill: a status's own color over its container,
// with a dot (or an icon, for a neutral state such as parked). The word tells it; the
// color only repeats it. A live state (a charge running) has its dot breathe, twice
// as it shows: a few seconds, so that nothing moves on without end (WCAG 2.2.2).
withDefaults(
  defineProps<{
    tone?: 'success' | 'warning' | 'error' | 'neutral'
    icon?: string
    live?: boolean
  }>(),
  { tone: 'neutral', icon: undefined, live: false },
)
</script>

<template>
  <span class="status-pill" :class="`status-pill--${tone}`">
    <v-icon v-if="icon" :icon size="16" aria-hidden="true" />
    <span v-else class="dot" :class="{ live }" aria-hidden="true" />
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
@keyframes breathe {
  from {
    scale: 1;
    opacity: 0.6;
  }
  to {
    scale: 2.6;
    opacity: 0;
  }
}
@media (prefers-reduced-motion: no-preference) {
  .dot.live {
    position: relative;
  }
  .dot.live::after {
    content: '';
    position: absolute;
    inset: 0;
    border-radius: 50%;
    background: currentColor;
    animation: breathe 1.8s ease-out 2 both;
  }
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
