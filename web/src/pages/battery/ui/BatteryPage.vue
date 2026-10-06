<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { AppShell } from '@/widgets/app-shell'
import { BatteryCharts } from '@/widgets/battery-charts'
import { BatteryTiles } from '@/widgets/battery-tiles'

// BatteryPage shows the estimated capacity of a vehicle over its whole history: no
// period is chosen, unlike the statistics. The three sentences under the title say where
// the estimate comes from and what it is not: a measurement.
defineProps<{ vehicle: string }>()
const { t } = useI18n()
</script>

<template>
  <AppShell :vehicle="vehicle">
    <div class="d-flex flex-wrap align-center justify-space-between ga-3 mb-4">
      <h1 class="text-headline-small">{{ t('battery.title') }}</h1>
    </div>
    <p class="intro text-body-medium text-medium-emphasis mb-4">{{ t('battery.intro') }}</p>
    <BatteryTiles :vehicle="vehicle" />
    <BatteryCharts :vehicle="vehicle" />
  </AppShell>
</template>
