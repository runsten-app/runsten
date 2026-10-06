<script setup lang="ts">
import { mdiOpenInNew } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTrip } from '@/features/browse-trips'
import { isApiError } from '@/shared/api'
import {
  formatCoordinate,
  formatCoordinates,
  openStreetMapUrl,
  useFormat,
  useMapsOn,
  type Position,
} from '@/shared/lib'
import { EventWhen, FactItem, MapView, ReconstructedBadge, type MapMarker } from '@/shared/ui'
import TripConsumption from './TripConsumption.vue'

// TripDetails is everything Runsten knows of a trip, and says what it does not.
const props = defineProps<{ vehicle: string; id: string }>()
const { t } = useI18n()
const mapsOn = useMapsOn()
const f = useFormat()
const { trip, isPending, error } = useTrip(
  () => props.vehicle,
  () => props.id,
)

// The heading names the day: times on it leave out their date.
const day = computed(() => (trip.value ? f.dayKey(trip.value.start.after) : undefined))
const positions = computed(() =>
  trip.value
    ? [
        {
          key: 'start',
          label: t('trip.startPosition'),
          position: trip.value.start_position,
          place: trip.value.start_place,
          address: trip.value.start_address,
        },
        {
          key: 'end',
          label: t('trip.endPosition'),
          position: trip.value.end_position,
          place: trip.value.end_place,
          address: trip.value.end_address,
        },
      ]
    : [],
)
// An end is named by its place, else by its address, else by its coordinates.
const name = (p: (typeof positions.value)[number]) => p.place?.name ?? p.address ?? null
// The map names each end so, else as start or end.
const markers = computed<MapMarker[]>(() =>
  positions.value.flatMap((p) =>
    p.position ? [{ position: p.position, label: name(p) ?? p.label }] : [],
  ),
)
// A position outside every place can become one, as a charge's can.
const newPlace = (p: Position) => ({
  name: 'place',
  params: { place: 'new' },
  query: { lat: formatCoordinate(p.lat), lon: formatCoordinate(p.lon) },
})
</script>

<template>
  <v-alert v-if="isApiError(error, 'not_found')" type="error" variant="tonal" class="not-found">
    {{ t('trip.notFound') }}
  </v-alert>
  <v-alert v-else-if="error" type="error" variant="tonal">{{ t('trip.error') }}</v-alert>
  <v-progress-linear v-else-if="isPending" indeterminate :aria-label="t('trip.loading')" />
  <article v-else-if="trip" class="trip-details">
    <header class="mb-4">
      <h1 class="text-headline-small">{{ t('trip.title') }}</h1>
      <p class="text-body-large text-medium-emphasis">{{ f.day(trip.start.after) }}</p>
    </header>

    <v-alert v-if="trip.reconstructed" type="info" variant="tonal" class="reconstructed-note mb-4">
      <div class="d-flex flex-wrap align-center ga-2 mb-1">
        <ReconstructedBadge />
        <EventWhen
          :start="trip.start"
          :end="trip.end"
          reconstructed
          :day
          class="text-title-medium"
        />
      </div>
      <p class="text-body-medium">{{ t('event.reconstructedHelp') }}</p>
    </v-alert>

    <v-row>
      <v-col v-if="!trip.reconstructed" cols="12" md="6">
        <v-card class="h-100 when">
          <v-card-title tag="h2" class="text-title-medium">{{ t('event.when') }}</v-card-title>
          <v-card-text>
            <dl class="facts">
              <FactItem :label="t('event.start')" :value="f.bounds(trip.start, day)" />
              <FactItem :label="t('event.end')" :value="f.bounds(trip.end, day)" />
              <FactItem :label="t('event.duration')" :value="f.duration(trip.start, trip.end)" />
            </dl>
            <p class="note text-body-small text-medium-emphasis mt-3">
              {{ t('event.boundsNote') }}
            </p>
          </v-card-text>
        </v-card>
      </v-col>

      <v-col cols="12" md="6">
        <v-card class="h-100 figures">
          <v-card-title tag="h2" class="text-title-medium">{{ t('trip.figures') }}</v-card-title>
          <v-card-text>
            <dl class="facts">
              <FactItem :label="t('trip.distance')" :value="f.quantity(trip.distance_km, 'km')" />
              <FactItem :label="t('trip.energy')" :value="f.quantity(trip.energy_kwh, 'kWh')" />
              <FactItem
                :label="t('trip.soc')"
                :value="f.change(trip.start_soc_pct, trip.end_soc_pct, 'percent')"
              />
              <FactItem
                :label="t('trip.range')"
                :value="f.change(trip.start_range_km, trip.end_range_km, 'km')"
              />
              <FactItem
                :label="t('trip.odometer')"
                :value="f.change(trip.start_odometer_km, trip.end_odometer_km, 'km')"
              />
            </dl>
            <p class="note text-body-small text-medium-emphasis mt-3">
              {{ t('trip.energyNote') }}
            </p>
          </v-card-text>
        </v-card>
      </v-col>

      <v-col cols="12" md="6">
        <TripConsumption :vehicle :trip />
      </v-col>

      <v-col cols="12" md="6">
        <v-card class="h-100 positions">
          <v-card-title tag="h2" class="text-title-medium">{{ t('trip.positions') }}</v-card-title>
          <v-card-text>
            <MapView v-if="markers.length" :label="t('trip.map')" :markers route class="mb-4" />
            <dl class="facts">
              <FactItem
                v-for="p in positions"
                :key="p.key"
                :label="p.label"
                :value="name(p) ?? f.coordinates(p.position)"
              >
                <span
                  v-if="name(p) && p.position"
                  class="coordinates d-block text-body-medium text-medium-emphasis mt-1"
                >
                  {{ formatCoordinates(p.position) }}
                </span>
                <div v-if="p.position" class="links d-flex flex-column align-start mt-1">
                  <v-btn
                    v-if="p.place"
                    :to="{ name: 'place', params: { place: p.place.id } }"
                    variant="text"
                    color="primary"
                    size="small"
                    class="place-link px-0"
                  >
                    {{ t('trip.openPlace') }}
                  </v-btn>
                  <v-btn
                    v-else
                    :to="newPlace(p.position)"
                    variant="text"
                    color="primary"
                    size="small"
                    class="create-place px-0"
                  >
                    {{ t('trip.createPlace') }}
                  </v-btn>
                  <v-btn
                    v-if="!mapsOn"
                    :href="openStreetMapUrl(p.position)"
                    target="_blank"
                    rel="noreferrer"
                    variant="text"
                    color="primary"
                    size="small"
                    :append-icon="mdiOpenInNew"
                    class="px-0"
                  >
                    {{ t('common.openInOsm') }}
                  </v-btn>
                </div>
              </FactItem>
            </dl>
          </v-card-text>
        </v-card>
      </v-col>

      <v-col cols="12" md="6">
        <v-card class="h-100 vehicle-reported">
          <v-card-title tag="h2" class="text-title-medium text-wrap">{{
            t('trip.vehicle.title')
          }}</v-card-title>
          <v-card-text>
            <dl class="facts">
              <FactItem
                :label="t('trip.vehicle.tripMeter')"
                :value="f.quantity(trip.vehicle_reported.trip_meter_km, 'km', 1)"
              />
              <FactItem
                :label="t('trip.vehicle.consumption')"
                :value="f.quantity(trip.vehicle_reported.consumption_kwh_per_100km, 'kWhPer100km')"
              />
            </dl>
            <p class="note text-body-small text-medium-emphasis mt-3">
              {{ t('trip.vehicle.note') }}
            </p>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>
  </article>
</template>

<style scoped>
.facts {
  display: grid;
  gap: 1rem;
}
</style>
