<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  activeWarnings,
  allFine,
  hasPressures,
  needsAttention,
  tyreTrouble,
} from '@/entities/health'
import { useHealth, useHealthText } from '@/features/view-health'
import { useFormat, useNow } from '@/shared/lib'
import { AppShell } from '@/widgets/app-shell'
import { BatterySummary } from '@/widgets/battery-tiles'
import { HealthTiles } from '@/widgets/health-tiles'
import { SoftwareHistory } from '@/widgets/software-history'

// The pressures' chart comes on demand, with Chart.js, and only for a vehicle that gives
// them: a Volvo gives a warning per tyre, never its pressure.
const TyreChart = defineAsyncComponent(() =>
  import('@/widgets/tyre-chart').then((m) => m.TyreChart),
)

// HealthPage shows the vehicle's health: under its title, what needs attention, and how
// fresh it is; its battery's estimated capacity, which leads to
// the battery's page, and the versions of its software when it tells them; then what it
// last told of its tyres, its service and its warnings, and, folded, the tyres' pressures
// over time. The battery's and the versions come first and stay whatever the vehicle told:
// they are derived from the charges and the readings.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const f = useFormat()
const now = useNow()
const { health, isPending, error } = useHealth(() => props.vehicle)
const text = useHealthText()
const known = computed(() => (health.value?.checked_at ? health.value : null))
// The page's answer first, under its title: what needs attention, named, or nothing to
// report; a health known in part claims neither. Then how fresh it is.
const verdict = computed(() => {
  const h = known.value
  if (!h) return null
  if (needsAttention(h)) {
    const s = h.service?.state
    const items = [
      ...tyreTrouble(h).map(
        (w) => `${text.wheels.value[w.wheel]} (${text.tyreWarnings.value[w.warning ?? 'ok']})`,
      ),
      ...(s === 'due' || s === 'overdue'
        ? [`${t('health.service.title')} (${text.serviceStates.value[s]})`]
        : []),
      ...activeWarnings(h).map((c) => text.checks.value[c.check]),
    ]
    return { attention: true, text: t('health.verdict.attention', { items: items.join(', ') }) }
  }
  return allFine(h) ? { attention: false, text: t('health.verdict.fine') } : null
})
</script>

<template>
  <AppShell :vehicle="vehicle">
    <h1 class="text-headline-small" :class="known ? 'mb-1' : 'mb-4'">{{ t('health.title') }}</h1>
    <p v-if="known" class="verdict text-body-medium mb-4">
      <span v-if="verdict" :class="{ attention: verdict.attention }">{{ verdict.text }}</span>
      <span v-if="verdict" class="text-medium-emphasis" aria-hidden="true"> · </span>
      <time class="text-medium-emphasis" :datetime="known.checked_at ?? undefined">{{
        t('state.lastChecked', { age: f.age(known.checked_at ?? '', now) })
      }}</time>
    </p>
    <BatterySummary :vehicle="vehicle" class="mb-3" />
    <SoftwareHistory :vehicle="vehicle" class="mb-3" />
    <v-alert v-if="error && !health" type="error" variant="tonal">{{ t('health.error') }}</v-alert>
    <v-progress-linear v-else-if="isPending" indeterminate :aria-label="t('health.loading')" />
    <p v-else-if="!known">{{ t('health.none') }}</p>
    <template v-else>
      <HealthTiles :health="known" />
      <!-- Folded: the pressures over time are for the curious; opened, the chart loads. -->
      <v-expansion-panels v-if="hasPressures(known)" class="pressures mt-3">
        <v-expansion-panel :title="t('health.chart.history')">
          <v-expansion-panel-text>
            <TyreChart :vehicle="vehicle" />
          </v-expansion-panel-text>
        </v-expansion-panel>
      </v-expansion-panels>
    </template>
  </AppShell>
</template>

<style scoped>
.attention {
  font-weight: 600;
}
</style>
