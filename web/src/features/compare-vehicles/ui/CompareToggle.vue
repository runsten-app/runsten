<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useGarageView } from '../composables/useGarageView'
import { compareViews, type CompareView } from '../model/garage'

// CompareToggle picks what the Garage compares: consumption, cost, charging or battery.
const { t } = useI18n()
const { view, setView } = useGarageView()
const labels = computed<Record<CompareView, string>>(() => ({
  consumption: t('garage.compare.consumption.tab'),
  cost: t('garage.compare.cost.tab'),
  charging: t('garage.compare.charging.tab'),
  battery: t('garage.compare.battery.tab'),
}))
</script>

<template>
  <v-btn-toggle
    :model-value="view"
    mandatory
    density="compact"
    class="segmented compare-toggle"
    role="group"
    :aria-label="t('garage.compare.label')"
    @update:model-value="(v: CompareView) => setView(v)"
  >
    <v-btn v-for="v in compareViews" :key="v" :value="v" :aria-pressed="v === view">
      {{ labels[v] }}
    </v-btn>
  </v-btn-toggle>
</template>
