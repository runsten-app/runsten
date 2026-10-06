<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RouteLocationRaw } from 'vue-router'
import { useFormat } from '@/shared/lib'
import { EventWhen, ReconstructedBadge, SpokenText } from '@/shared/ui'
import type { Trip } from '../model/Trip'

// TripSummary is a trip in a list, a link to its details: when, from where to where
// when a place or an address names either end, how far, how long.
const props = defineProps<{ trip: Trip; to: RouteLocationRaw; day?: string }>()
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
    <template v-if="trip.reconstructed" #append>
      <ReconstructedBadge />
    </template>
  </v-list-item>
</template>
