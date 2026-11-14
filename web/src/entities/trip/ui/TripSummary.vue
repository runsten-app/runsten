<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RouteLocationRaw } from 'vue-router'
import { useFormat } from '@/shared/lib'
import { EventWhen, ImportedBadge, ReconstructedBadge, SpokenText } from '@/shared/ui'
import type { Trip } from '../model/Trip'

// TripSummary is a trip in a list, a link to its details: when, from where to where
// when a place or an address names either end, how far, how long; with costs, what it
// cost and its cost per 100 km, on its right ("Unknown cost" without one).
const props = defineProps<{ trip: Trip; to: RouteLocationRaw; day?: string; costs?: boolean }>()
const { t } = useI18n()
const f = useFormat()

const figures = computed(() => {
  const trip = props.trip
  const out = [
    trip.distance_km === null ? t('trip.distanceUnknown') : f.quantity(trip.distance_km, 'km'),
  ]
  // A reconstructed trip has no duration: it lies somewhere within its interval.
  if (!trip.reconstructed) out.push(f.duration(trip.start, trip.end))
  if (trip.start_soc_pct !== null || trip.end_soc_pct !== null)
    out.push(f.change(trip.start_soc_pct, trip.end_soc_pct, 'percent'))
  return out.join(' · ')
})
const cost = computed(() => {
  const c = props.trip.cost
  if (!c) return null
  const amount = f.estimate(c, c.currency)
  const per100 = c.per_100km && f.estimate(c.per_100km, c.currency)
  return {
    amount,
    per100: per100 && {
      text: t('trip.cost.per100km', { amount: per100.text }),
      spoken: t('trip.cost.per100kmSpoken', { amount: per100.spoken }),
    },
  }
})
const route = computed(() => {
  const trip = props.trip
  const from = trip.start_place?.name ?? trip.start_address
  const to = trip.end_place?.name ?? trip.end_address
  if (!from && !to) return null
  const names = { from: from ?? t('trip.elsewhere'), to: to ?? t('trip.elsewhere') }
  return { text: t('trip.route', names), spoken: t('trip.routeSpoken', names) }
})
</script>

<template>
  <v-list-item :to="to" :lines="route ? 'three' : 'two'" class="trip-summary">
    <v-list-item-title class="text-title-medium">
      <EventWhen :start="trip.start" :end="trip.end" :reconstructed="trip.reconstructed" :day />
    </v-list-item-title>
    <v-list-item-subtitle v-if="route" class="route">
      <SpokenText :text="route.text" :spoken="route.spoken" />
    </v-list-item-subtitle>
    <v-list-item-subtitle class="figures">{{ figures }}</v-list-item-subtitle>
    <template v-if="trip.reconstructed || trip.imported || costs" #append>
      <div class="d-flex align-center ga-3">
        <ReconstructedBadge v-if="trip.reconstructed" />
        <ImportedBadge v-else-if="trip.imported" :source="trip.sources[0] ?? ''" />
        <div v-if="costs" class="trip-cost text-end">
          <template v-if="cost">
            <SpokenText
              class="amount d-block text-body-large"
              :text="cost.amount.text"
              :spoken="cost.amount.spoken"
            />
            {{ ' ' }}
            <SpokenText
              v-if="cost.per100"
              class="per-100km d-block text-body-small text-medium-emphasis"
              :text="cost.per100.text"
              :spoken="cost.per100.spoken"
            />
          </template>
          <span v-else class="unknown text-body-medium text-medium-emphasis">{{
            t('trip.cost.unknown')
          }}</span>
        </div>
      </div>
    </template>
  </v-list-item>
</template>

<style scoped>
.trip-cost {
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}
</style>
