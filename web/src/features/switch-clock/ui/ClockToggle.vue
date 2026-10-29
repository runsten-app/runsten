<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { hourCycle, hourCycles, setHourCycle, type HourCycle } from '@/shared/lib'

// ClockToggle picks how times are written: as the language and region write them, or
// always on 24 or 12 hours. Kept in this browser; the focus stays on the button pressed.
const { t } = useI18n()
const labels = computed<Record<HourCycle, string>>(() => ({
  auto: t('preferences.clock.auto'),
  h23: t('preferences.clock.h23'),
  h12: t('preferences.clock.h12'),
}))
</script>

<template>
  <v-btn-toggle
    :model-value="hourCycle"
    mandatory
    density="compact"
    class="segmented clock-toggle"
    role="group"
    :aria-label="t('preferences.clock.title')"
    @update:model-value="setHourCycle"
  >
    <v-btn v-for="c in hourCycles" :key="c" :value="c" :aria-pressed="c === hourCycle">
      {{ labels[c] }}
    </v-btn>
  </v-btn-toggle>
</template>
