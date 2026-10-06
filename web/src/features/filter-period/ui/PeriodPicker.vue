<script setup lang="ts">
import { mdiChevronLeft, mdiChevronRight, mdiMenuDown } from '@mdi/js'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { currentShortcut, stepPeriod, useFormat, wholeMonths } from '@/shared/lib'
import { usePeriod } from '../composables/usePeriod'
import PeriodFilter from './PeriodFilter.vue'
import PeriodShortcuts from './PeriodShortcuts.vue'

// PeriodPicker is the period of a page in one line: its name ("October 2026"), which
// opens the usual periods and the exact days, and the periods just before and after, of
// the same length. The shortcuts and the days stay as they were, in the menu; the line
// keeps the page's first screen for its figures. Without clearable, the days cannot be
// emptied (the statistics always have a period).
const { clearable = true } = defineProps<{ clearable?: boolean }>()
const { t } = useI18n()
const f = useFormat()
const { days, period, setPeriod } = usePeriod()
const open = ref(false)
const today = new Date()

const label = computed(() => {
  const d = days.value
  if (!period.value.valid) return t('period.choose')
  const shortcut = currentShortcut(d, today)
  if (shortcut === 'all') return t('period.whole')
  if (shortcut === 'last12Months') return t('period.shortcut.last12Months')
  const months = wholeMonths(d)
  if (d.from && months === 1) return f.calendarMonth(d.from)
  if (d.from && months === 12 && d.from.endsWith('-01-01')) return d.from.slice(0, 4)
  if (d.from && d.to) return d.from === d.to ? f.calendarDay(d.from) : f.calendarRange(d.from, d.to)
  if (d.from) return t('period.since', { day: f.calendarDay(d.from) })
  return t('period.until', { day: f.calendarDay(d.to ?? '') })
})

// The arrows exist for a closed period only: before or after "everything" is nothing.
const previous = computed(() => (period.value.valid ? stepPeriod(days.value, -1) : undefined))
const next = computed(() => (period.value.valid ? stepPeriod(days.value, 1) : undefined))

// An invalid period, from the URL, is told under the line: the fields are in the menu.
const error = computed(() => {
  const p = period.value
  if (p.valid) return undefined
  return p.error === 'inverted' ? t('period.error.inverted') : t('period.error.invalidDate')
})
</script>

<template>
  <div class="period-picker">
    <div class="d-flex align-center ga-1">
      <v-btn
        v-if="previous"
        :icon="mdiChevronLeft"
        :aria-label="t('period.previous')"
        :title="t('period.previous')"
        variant="text"
        class="previous"
        @click="previous && setPeriod(previous)"
      />
      <v-menu v-model="open" :close-on-content-click="false" location="bottom start">
        <template #activator="{ props: menu }">
          <v-btn
            v-bind="menu"
            :append-icon="mdiMenuDown"
            color="surface"
            height="40"
            class="current"
          >
            <span class="d-sr-only">{{ t('period.shortcut.label') }}: </span>{{ label }}
          </v-btn>
        </template>
        <v-card
          class="period-menu"
          elevation="8"
          role="dialog"
          :aria-label="t('period.shortcut.label')"
        >
          <v-card-text>
            <PeriodShortcuts class="mb-4" @pick="open = false" />
            <PeriodFilter :clearable />
          </v-card-text>
        </v-card>
      </v-menu>
      <v-btn
        v-if="next"
        :icon="mdiChevronRight"
        :aria-label="t('period.next')"
        :title="t('period.next')"
        variant="text"
        class="next"
        @click="next && setPeriod(next)"
      />
    </div>
    <p v-if="error" class="period-error text-body-medium text-error mt-1 mb-0">{{ error }}</p>
  </div>
</template>

<style scoped>
/* An overlay over tiles of its own color: its shadow alone tells it apart, the one
   raised surface of the interface, with the dialogs' radius. */
.period-menu {
  max-inline-size: min(36rem, calc(100vw - 24px));
  border-radius: var(--v-tile-radius);
}
</style>
