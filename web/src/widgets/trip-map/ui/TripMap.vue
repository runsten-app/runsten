<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TripMap } from '@/entities/trip'
import { useTripMap } from '@/features/browse-trips'
import { formatCoordinates, useFormat } from '@/shared/lib'
import { MapView, type MapLink, type MapSpot } from '@/shared/ui'

// TripMap is where the vehicle went in the list's period: the ends of its trips gathered
// into spots, and a line between two spots for the trips between them, either way, drawn
// the thicker the more often it was driven. Runsten reads the ends of a trip, not its path:
// the lines are straight. The most driven links follow, in words.
const props = defineProps<{ vehicle: string; from?: string; to?: string }>()
const { t } = useI18n()
const f = useFormat()
const { map } = useTripMap(
  () => props.vehicle,
  () => props.from,
  () => props.to,
)

type Spot = TripMap['spots'][number]
const nameOf = (s: Spot) => s.place?.name ?? s.address ?? formatCoordinates(s.position)
const named = computed(() => (map.value?.spots ?? []).map(nameOf))

const spots = computed<MapSpot[]>(() => {
  const all = map.value?.spots ?? []
  const top = Math.max(1, ...all.map((s) => s.departures + s.arrivals))
  return all.map((s, i) => ({
    position: s.position,
    label: t(
      'tripMap.spot',
      { name: named.value[i] ?? '', count: s.departures + s.arrivals },
      s.departures + s.arrivals,
    ),
    weight: Math.sqrt((s.departures + s.arrivals) / top),
  }))
})

const links = computed(() => {
  const m = map.value
  if (!m) return []
  return m.links.flatMap((l) => {
    const a = m.spots[l.a]
    const b = m.spots[l.b]
    if (!a || !b) return []
    return [{ link: l, a, b, from: named.value[l.a] ?? '', to: named.value[l.b] ?? '' }]
  })
})

const drawn = computed<MapLink[]>(() => {
  const top = Math.max(1, ...links.value.map((l) => l.link.trips))
  return links.value.map(({ link, a, b, from, to }) => ({
    from: a.position,
    to: b.position,
    label: `${t('tripMap.between', { from, to })} · ${t('tripMap.trips', link.trips)}`,
    weight: Math.sqrt(link.trips / top),
  }))
})

// The journeys made again: a link driven once is on the map, not in the list.
const frequent = computed(() => links.value.filter((l) => l.link.trips > 1).slice(0, 5))

const summary = computed(() => {
  const m = map.value
  if (!m) return ''
  return [
    t('tripMap.trips', m.trips),
    t('tripMap.spots', m.spots.length),
    t('tripMap.links', m.links.length),
  ].join(' · ')
})
</script>

<template>
  <!-- Nothing to draw: the list says the period is empty. -->
  <v-card v-if="map && map.trips > 0" class="trip-map mb-4">
    <v-card-title tag="h2">{{ t('tripMap.title') }}</v-card-title>
    <v-card-text>
      <p class="text-body-medium mb-3">{{ summary }}</p>
      <MapView :label="t('tripMap.label')" :spots :links="drawn" tall />
      <p class="text-body-small text-medium-emphasis mt-2 mb-0">
        {{ t('tripMap.hint') }}
        <template v-if="map.trips_without_position">
          {{ t('tripMap.withoutPosition', map.trips_without_position) }}
        </template>
      </p>
      <template v-if="frequent.length">
        <h3 class="text-title-small mt-4 mb-1">{{ t('tripMap.frequent') }}</h3>
        <ol class="frequent">
          <li v-for="l in frequent" :key="`${l.link.a}-${l.link.b}`" class="py-1">
            <span class="route">{{ t('tripMap.between', { from: l.from, to: l.to }) }}</span>
            <!-- A space for whoever reads the text: the gap of the flex box is drawn only. -->
            {{ ' ' }}
            <span class="text-medium-emphasis">
              {{ t('tripMap.trips', l.link.trips) }} · {{ f.quantity(l.link.distance_km, 'km') }}
            </span>
          </li>
        </ol>
      </template>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.frequent {
  list-style: none;
  padding: 0;
  margin: 0;
}
.frequent li {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  column-gap: 1rem;
}
.frequent li + li {
  border-top: thin solid rgba(var(--v-border-color), var(--v-border-opacity));
}
.route {
  min-width: 0;
  overflow-wrap: anywhere;
}
</style>
