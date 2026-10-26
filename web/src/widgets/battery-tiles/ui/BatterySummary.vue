<script setup lang="ts">
import { mdiArrowRight } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useBattery } from '@/features/view-battery'
import { useFormat } from '@/shared/lib'
import { TileFigure } from '@/shared/ui'
import { remainingOf } from '../model/remaining'

// BatterySummary tells on the health page the battery's estimated capacity, an estimate
// among the health's figures, and leads to the battery's page: its estimates, its charts.
// Without an answer, the link alone: the health stays whole.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const f = useFormat()
const { battery } = useBattery(() => props.vehicle)

const figure = computed(() => {
  const b = battery.value
  if (!b) return null
  const remaining = remainingOf(b)
  if (remaining)
    return {
      label: t('battery.tile.remaining'),
      value: t('battery.tile.remainingValue', { pct: f.quantity(remaining.pct, 'percent') }),
      known: true,
    }
  if (b.current)
    return {
      label: t('battery.tile.capacity'),
      value: f.quantity(b.current.capacity_kwh, 'kWh'),
      known: true,
    }
  return { label: t('battery.tile.capacity'), value: t('battery.tile.notEnough'), known: false }
})
</script>

<template>
  <v-card class="tile battery-summary">
    <v-card-title tag="h2">{{ t('battery.title') }}</v-card-title>
    <v-card-text>
      <dl v-if="figure" class="mb-3">
        <div>
          <dt class="text-medium-emphasis">{{ figure.label }}</dt>
          <dd>
            <TileFigure
              class="value"
              :text="figure.value"
              :size="figure.known ? 'kpi' : 'small'"
              :unknown="!figure.known"
            />
          </dd>
        </div>
      </dl>
      <router-link :to="{ name: 'battery', params: { vehicle } }" class="text-primary">
        {{ t('battery.state.open') }}
        <v-icon :icon="mdiArrowRight" size="16" aria-hidden="true" />
      </router-link>
    </v-card-text>
  </v-card>
</template>

<style scoped>
dl,
dd {
  margin: 0;
}
dt {
  font-size: 0.8125rem;
  margin-bottom: 6px;
}
</style>
