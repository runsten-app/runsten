<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RouteLocationRaw } from 'vue-router'
import { useFormat } from '@/shared/lib'
import { EventWhen, ReconstructedBadge, SpokenText } from '@/shared/ui'
import type { Charge } from '../model/Charge'

// ChargeSummary is a charge in a list, a link to its details: when, how much, how long,
// and its cost when it is known (an unknown one is told in the details).
const props = defineProps<{ charge: Charge; to: RouteLocationRaw; day?: string }>()
const { t } = useI18n()
const f = useFormat()

const figures = computed(() => {
  const c = props.charge
  const out: string[] = []
  if (c.type) out.push(c.type)
  out.push(
    c.start_soc_pct === null && c.end_soc_pct === null
      ? t('charge.socUnknown')
      : f.change(c.start_soc_pct, c.end_soc_pct, 'percent'),
  )
  // A reconstructed charge has no duration: it lies somewhere within its interval.
  if (!c.reconstructed) out.push(f.duration(c.start, c.end))
  return out.join(' · ')
})
const cost = computed(() => (props.charge.cost ? f.chargeCost(props.charge.cost) : null))
</script>

<template>
  <v-list-item :to="to" lines="two" class="charge-summary">
    <v-list-item-title class="text-title-medium">
      <EventWhen
        :start="charge.start"
        :end="charge.end"
        :reconstructed="charge.reconstructed"
        :day
      />
    </v-list-item-title>
    <v-list-item-subtitle class="figures">{{ figures }}</v-list-item-subtitle>
    <template v-if="charge.reconstructed || cost" #append>
      <div class="d-flex flex-column align-end ga-1">
        <SpokenText
          v-if="cost"
          class="cost text-body-large"
          :text="cost.text"
          :spoken="cost.spoken"
        />
        <ReconstructedBadge v-if="charge.reconstructed" />
      </div>
    </template>
  </v-list-item>
</template>

<style scoped>
.cost {
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
</style>
