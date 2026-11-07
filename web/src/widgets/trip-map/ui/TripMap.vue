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
const { map, error } = useTripMap(
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
    label: `${named.value[i] ?? ''}: ${t('tripMap.departures', s.departures)}, ${t('tripMap.arrivals', s.arrivals)}`,
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

// The journeys made again show; every spot and link is in the folded list below.
const frequent = computed(() => links.value.filter((l) => l.link.trips > 1).slice(0, 5))

const shown = computed(() => !!map.value && map.value.trips + map.value.trips_without_position > 0)

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
  <v-alert v-if="error && !map" type="error" variant="tonal" class="trip-map-error mb-4">
    {{ t('tripMap.error') }}
  </v-alert>
  <!-- Nothing to draw: the list says the period is empty. -->
  <v-card v-else-if="map && shown" class="trip-map mb-4">
    <v-card-title tag="h2">{{ t('tripMap.title') }}</v-card-title>
    <v-card-text>
      <p class="text-body-medium mt-0 mb-3">{{ summary }}</p>
      <MapView v-if="map.trips > 0" :label="t('tripMap.label')" :spots :links="drawn" tall />
      <p class="text-body-small text-medium-emphasis mt-2 mb-0">
        <template v-if="map.trips > 0">{{ t('tripMap.hint') }}</template>
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
              <template v-if="l.link.distance_unknown">
                · {{ t('stats.trips.distanceUnknown', l.link.distance_unknown) }}
              </template>
            </span>
          </li>
        </ol>
      </template>
      <!-- What the map draws, in words, for whoever cannot read it: every spot with its
           departures and arrivals, every link, those driven once too. Folded, so that the
           frequent ones lead; a <details> opens from the keyboard. -->
      <details v-if="map.spots.length" class="every mt-4">
        <summary class="text-title-small">{{ t('tripMap.every') }}</summary>
        <h3 class="text-title-small mt-3 mb-1">{{ t('tripMap.everySpots') }}</h3>
        <ol class="spots">
          <li v-for="(s, i) in map.spots" :key="i">
            {{ named[i] }}: {{ t('tripMap.departures', s.departures) }},
            {{ t('tripMap.arrivals', s.arrivals) }}
          </li>
        </ol>
        <template v-if="links.length">
          <h3 class="text-title-small mt-3 mb-1">{{ t('tripMap.everyJourneys') }}</h3>
          <ol class="journeys">
            <li v-for="l in links" :key="`${l.link.a}-${l.link.b}`">
              {{ t('tripMap.between', { from: l.from, to: l.to }) }}:
              {{ t('tripMap.trips', l.link.trips) }}, {{ f.quantity(l.link.distance_km, 'km') }}
              <template v-if="l.link.distance_unknown">
                , {{ t('stats.trips.distanceUnknown', l.link.distance_unknown) }}
              </template>
            </li>
          </ol>
        </template>
      </details>
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
.every summary {
  cursor: pointer;
  width: fit-content;
}
.route {
  min-width: 0;
  overflow-wrap: anywhere;
}
</style>
