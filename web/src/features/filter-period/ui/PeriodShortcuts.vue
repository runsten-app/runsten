<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { currentShortcut, shortcutDays, shortcuts, type Shortcut } from '@/shared/lib'
import { usePeriod } from '../composables/usePeriod'

// PeriodShortcuts sets the usual periods in one press. Buttons, each saying whether its
// period is the one shown: the focus stays on the one pressed, since only the URL's
// query changes. In the period's menu, a tile: its buttons are insets. pick tells the period picker, which closes its menu.
const emit = defineEmits<{ pick: [] }>()
const { t } = useI18n()
const { days, setPeriod } = usePeriod()
const today = new Date()
const current = computed(() => currentShortcut(days.value, today))
const labels = computed<Record<Shortcut, string>>(() => ({
  thisMonth: t('period.shortcut.thisMonth'),
  lastMonth: t('period.shortcut.lastMonth'),
  thisYear: t('period.shortcut.thisYear'),
  last12Months: t('period.shortcut.last12Months'),
  all: t('period.shortcut.all'),
}))
</script>

<template>
  <div
    class="period-shortcuts d-flex flex-wrap ga-2"
    role="group"
    :aria-label="t('period.shortcut.label')"
  >
    <v-btn
      v-for="s in shortcuts"
      :key="s"
      :aria-pressed="s === current"
      color="surface-variant"
      height="36"
      @click="(setPeriod(shortcutDays(s, today)), emit('pick'))"
    >
      {{ labels[s] }}
    </v-btn>
  </div>
</template>

<style scoped>
/* The period shown is inverted: the text color's fill, the ground's letters. */
.v-btn[aria-pressed='true'] {
  background: rgb(var(--v-theme-on-surface)) !important;
  color: rgb(var(--v-theme-background)) !important;
}
</style>
