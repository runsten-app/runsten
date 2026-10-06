<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { capacityAxes, useCapacityAxis, type CapacityAxis } from '../composables/useCapacityAxis'

// AxisToggle picks what the capacity chart is plotted against: the date or the mileage.
// Only the URL's query changes, so the focus stays on the button pressed.
const { t } = useI18n()
const { axis, setAxis } = useCapacityAxis()
const labels = computed<Record<CapacityAxis, string>>(() => ({
  date: t('battery.axis.date'),
  odometer: t('battery.axis.odometer'),
}))
</script>

<template>
  <v-btn-toggle
    :model-value="axis"
    mandatory
    density="compact"
    class="segmented axis-toggle"
    role="group"
    :aria-label="t('battery.axis.label')"
    @update:model-value="(a: CapacityAxis) => setAxis(a)"
  >
    <v-btn v-for="a in capacityAxes" :key="a" :value="a" :aria-pressed="a === axis">
      {{ labels[a] }}
    </v-btn>
  </v-btn-toggle>
</template>
