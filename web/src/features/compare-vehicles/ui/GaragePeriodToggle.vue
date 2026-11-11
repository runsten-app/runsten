<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useGarageView } from '../composables/useGarageView'
import { garagePeriods, type GaragePeriod } from '../model/garage'

// GaragePeriodToggle picks the Garage's period: the last 30 days or the last 12 months.
// The totals, the tiles and the comparison follow it; the spending per month does not.
const { t } = useI18n()
const { period, setPeriod } = useGarageView()
const labels = computed<Record<GaragePeriod, string>>(() => ({
  '30d': t('garage.period.30d'),
  '12m': t('garage.period.12m'),
}))
</script>

<template>
  <v-btn-toggle
    :model-value="period"
    mandatory
    density="compact"
    class="segmented garage-period"
    role="group"
    :aria-label="t('garage.period.label')"
    @update:model-value="(p: GaragePeriod) => setPeriod(p)"
  >
    <v-btn v-for="p in garagePeriods" :key="p" :value="p" :aria-pressed="p === period">
      {{ labels[p] }}
    </v-btn>
  </v-btn-toggle>
</template>
