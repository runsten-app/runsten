<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RouteLocationRaw } from 'vue-router'
import { useFormat } from '@/shared/lib'
import { SpokenText, StatusPill } from '@/shared/ui'
import type { ChargeInProgress } from '../model/Charge'

// ChargeInProgressSummary is the charge in progress at the top of a list, a link to its
// live details: since when, how much so far, and its cost so far. stale says the readings
// stopped: then it is never said to go on, only how long ago it was last read.
const props = defineProps<{
  charge: ChargeInProgress
  to: RouteLocationRaw
  stale: boolean
  now: number
}>()
const { t } = useI18n()
const f = useFormat()

const figures = computed(() => {
  const c = props.charge
  const out: string[] = []
  if (c.type) out.push(c.type)
  out.push(
    c.start_soc_pct === null && c.soc_pct === null
      ? t('charge.socUnknown')
      : f.change(c.start_soc_pct, c.soc_pct, 'percent'),
  )
  const energy = c.energy_measured_kwh ?? c.energy_soc_kwh
  if (energy !== null) out.push(t('chargeInProgress.added', { energy: f.quantity(energy, 'kWh') }))
  if (props.stale) out.push(t('chargeInProgress.readAgo', { age: f.age(c.read_at, props.now) }))
  return out.join(' · ')
})
const cost = computed(() => (props.charge.cost ? f.chargeCost(props.charge.cost) : null))
</script>

<template>
  <v-list-item :to="to" lines="two" class="charge-in-progress-summary">
    <v-list-item-title class="d-flex flex-wrap align-center ga-2 text-title-medium">
      <StatusPill :tone="stale ? 'warning' : 'success'" class="state">
        {{ stale ? t('chargeInProgress.stopped') : t('chargeInProgress.label') }}
      </StatusPill>
      <span>{{ t('chargeInProgress.since', { time: f.bounds(charge.start) }) }}</span>
    </v-list-item-title>
    <v-list-item-subtitle class="figures">{{ figures }}</v-list-item-subtitle>
    <template v-if="cost" #append>
      <SpokenText class="cost text-body-large" :text="cost.text" :spoken="cost.spoken" />
    </template>
  </v-list-item>
</template>

<style scoped>
.cost {
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
</style>
