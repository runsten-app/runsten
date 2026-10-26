<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import { useI18n } from 'vue-i18n'
import { hasPressures } from '@/entities/health'
import { useHealth } from '@/features/view-health'
import { useFormat, useNow } from '@/shared/lib'
import { AppShell } from '@/widgets/app-shell'
import { BatterySummary } from '@/widgets/battery-tiles'
import { HealthTiles } from '@/widgets/health-tiles'

// The pressures' chart comes on demand, with Chart.js, and only for a vehicle that gives
// them: a Volvo gives a warning per tyre, never its pressure.
const TyreChart = defineAsyncComponent(() =>
  import('@/widgets/tyre-chart').then((m) => m.TyreChart),
)

// HealthPage shows the vehicle's health: its battery's estimated capacity, which leads to
// the battery's page, what it last told of its tyres, its service and its warnings, and
// the tyres' pressures over time. The battery's comes first and stays whatever the
// vehicle told: it is derived from the charges.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const f = useFormat()
const now = useNow()
const { health, isPending, error } = useHealth(() => props.vehicle)
const known = computed(() => (health.value?.checked_at ? health.value : null))
</script>

<template>
  <AppShell :vehicle="vehicle">
    <h1 class="text-headline-small mb-2">{{ t('health.title') }}</h1>
    <p class="intro text-body-medium text-medium-emphasis mb-4">{{ t('health.intro') }}</p>
    <BatterySummary :vehicle="vehicle" class="mb-3" />
    <v-alert v-if="error && !health" type="error" variant="tonal">{{ t('health.error') }}</v-alert>
    <v-progress-linear v-else-if="isPending" indeterminate :aria-label="t('health.loading')" />
    <p v-else-if="!known">{{ t('health.none') }}</p>
    <template v-else>
      <p class="checked text-medium-emphasis mb-3">
        <time :datetime="known.checked_at ?? undefined">{{
          t('state.lastChecked', { age: f.age(known.checked_at ?? '', now) })
        }}</time>
      </p>
      <HealthTiles :health="known" />
      <TyreChart v-if="hasPressures(known)" :vehicle="vehicle" class="mt-6" />
    </template>
  </AppShell>
</template>

<style scoped>
.intro {
  max-inline-size: 70ch;
}
.checked {
  font-size: 0.8125rem;
}
</style>
