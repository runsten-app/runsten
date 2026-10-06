<script setup lang="ts">
import { mdiMapMarkerPlus, mdiOpenInNew } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useCharge } from '@/features/browse-charges'
import { useSettings } from '@/features/edit-settings'
import { ChargeCostEditor } from '@/features/enter-charge-cost'
import { FeatureUnavailable, useLimits } from '@/features/view-limits'
import { isApiError } from '@/shared/api'
import { formatCoordinate, openStreetMapUrl, useFormat, useMapsOn } from '@/shared/lib'
import { EventWhen, FactItem, MapView, ReconstructedBadge } from '@/shared/ui'

// ChargeDetails is everything Runsten knows of a charge, and says what it does not. Its
// cost, where the account's offer leaves the costs out, is said to be part of another.
const props = defineProps<{ vehicle: string; id: string }>()
const { t } = useI18n()
const mapsOn = useMapsOn()
const f = useFormat()
const { charge, isPending, error } = useCharge(
  () => props.vehicle,
  () => props.id,
)

const { settings } = useSettings()
const { lacks } = useLimits()
const noCosts = computed(() => lacks('costs'))
const currency = computed(() => settings.value?.currency ?? null)

// The heading names the day: times on it leave out their date.
const day = computed(() => (charge.value ? f.dayKey(charge.value.start.after) : undefined))

const placeRoute = computed(() => {
  const c = charge.value
  if (c?.place) return { name: 'place', params: { place: c.place.id } }
  if (c?.position)
    return {
      name: 'place',
      params: { place: 'new' },
      query: {
        lat: formatCoordinate(c.position.lat),
        lon: formatCoordinate(c.position.lon),
      },
    }
  return undefined
})

const cost = computed(() => f.chargeCost(charge.value?.cost ?? null))
// costNotes say where the cost comes from, and why it is a range: an estimate from a
// tariff (billed energy, assumed efficiency) or what was entered.
const costNotes = computed(() => {
  const c = charge.value?.cost
  if (!c) {
    if (!settings.value) return []
    if (!currency.value) return [t('charge.cost.noCurrency')]
    // At a place, with its energy and no entered cost: only a price is missing, before
    // the first of the tariff.
    const place = charge.value?.place
    if (place && charge.value?.energy_soc_kwh !== null)
      return [t('charge.cost.unpriced', { place: place.name })]
    return [t('charge.cost.unknown')]
  }
  if (c.source === 'entered') {
    const notes = [t('charge.cost.entered')]
    if (c.energy_kwh !== null)
      notes.push(t('charge.cost.billed', { energy: f.quantity(c.energy_kwh, 'kWh') }))
    if (c.note) notes.push(t('charge.cost.note', { note: c.note }))
    return notes
  }
  const notes = [t('charge.cost.tariff', { place: charge.value?.place?.name ?? '' })]
  if (c.energy_kwh !== null && c.efficiency !== null)
    notes.push(
      t('charge.cost.estimate', {
        energy: f.quantity(c.energy_kwh, 'kWh'),
        efficiency: f.quantity(c.efficiency * 100, 'percent'),
      }),
    )
  if (c.min_minor !== c.max_minor) notes.push(t('charge.cost.range'))
  return notes
})
</script>

<template>
  <v-alert v-if="isApiError(error, 'not_found')" type="error" variant="tonal" class="not-found">
    {{ t('charge.notFound') }}
  </v-alert>
  <v-alert v-else-if="error" type="error" variant="tonal">{{ t('charge.error') }}</v-alert>
  <v-progress-linear v-else-if="isPending" indeterminate :aria-label="t('charge.loading')" />
  <article v-else-if="charge" class="charge-details">
    <header class="mb-4">
      <h1 class="text-headline-small">{{ t('charge.title') }}</h1>
      <p class="text-body-large text-medium-emphasis">{{ f.day(charge.start.after) }}</p>
    </header>

    <v-alert
      v-if="charge.reconstructed"
      type="info"
      variant="tonal"
      class="reconstructed-note mb-4"
    >
      <div class="d-flex flex-wrap align-center ga-2 mb-1">
        <ReconstructedBadge />
        <EventWhen
          :start="charge.start"
          :end="charge.end"
          reconstructed
          :day
          class="text-title-medium"
        />
      </div>
      <p class="text-body-medium">{{ t('event.reconstructedHelp') }}</p>
    </v-alert>

    <v-row>
      <v-col v-if="!charge.reconstructed" cols="12" md="6">
        <v-card class="h-100 when">
          <v-card-title tag="h2" class="text-title-medium">{{ t('event.when') }}</v-card-title>
          <v-card-text>
            <dl class="facts">
              <FactItem :label="t('event.start')" :value="f.bounds(charge.start, day)" />
              <FactItem :label="t('event.end')" :value="f.bounds(charge.end, day)" />
              <FactItem
                :label="t('event.duration')"
                :value="f.duration(charge.start, charge.end)"
              />
            </dl>
            <p class="note text-body-small text-medium-emphasis mt-3">
              {{ t('event.boundsNote') }}
            </p>
          </v-card-text>
        </v-card>
      </v-col>

      <v-col cols="12" md="6">
        <v-card class="h-100 figures">
          <v-card-title tag="h2" class="text-title-medium">{{ t('charge.figures') }}</v-card-title>
          <v-card-text>
            <dl class="facts">
              <FactItem :label="t('charge.type')" :value="charge.type ?? f.unknown()" />
              <FactItem
                :label="t('charge.soc')"
                :value="f.change(charge.start_soc_pct, charge.end_soc_pct, 'percent')"
              />
              <FactItem
                :label="t('charge.target')"
                :value="f.quantity(charge.target_soc_pct, 'percent')"
              />
            </dl>
          </v-card-text>
        </v-card>
      </v-col>

      <v-col cols="12" md="6">
        <v-card class="h-100 energies">
          <v-card-title tag="h2" class="text-title-medium">{{ t('charge.energy') }}</v-card-title>
          <v-card-text>
            <dl class="facts">
              <FactItem
                :label="t('charge.energySoc')"
                :value="f.quantity(charge.energy_soc_kwh, 'kWh')"
              />
              <FactItem
                :label="t('charge.energyPower')"
                :value="f.quantity(charge.energy_power_kwh, 'kWh')"
              />
            </dl>
            <p class="note text-body-small text-medium-emphasis mt-3">
              {{ t('charge.energyNote') }}
            </p>
          </v-card-text>
        </v-card>
      </v-col>

      <v-col cols="12" md="6">
        <v-card class="h-100 cost">
          <v-card-title tag="h2" class="text-title-medium">{{
            t('charge.cost.title')
          }}</v-card-title>
          <v-card-text>
            <dl class="facts">
              <FactItem
                :label="t('charge.cost.place')"
                :value="charge.place?.name ?? t('charge.cost.noPlace')"
              >
                <router-link
                  v-if="charge.place && placeRoute"
                  :to="placeRoute"
                  class="place-link d-block text-body-medium text-primary mt-1"
                >
                  {{ t('charge.cost.openPlace') }}
                </router-link>
              </FactItem>
              <FactItem
                v-if="!noCosts"
                :label="t('charge.cost.label')"
                :value="cost.text"
                :spoken="cost.spoken"
              >
                <span
                  v-for="n in costNotes"
                  :key="n"
                  class="cost-note d-block text-body-medium text-medium-emphasis mt-1"
                >
                  {{ n }}
                </span>
              </FactItem>
            </dl>
            <FeatureUnavailable v-if="noCosts" feature="costs" class="mt-4" />
            <div
              v-if="(settings && !currency && !noCosts) || (!charge.place && placeRoute)"
              class="d-flex flex-wrap ga-2 mt-4"
            >
              <v-btn
                v-if="settings && !currency && !noCosts"
                :to="{ name: 'settings' }"
                variant="tonal"
                color="primary"
                class="choose-currency"
              >
                {{ t('charge.cost.chooseCurrency') }}
              </v-btn>
              <v-btn
                v-if="!charge.place && placeRoute"
                :to="placeRoute"
                :prepend-icon="mdiMapMarkerPlus"
                variant="tonal"
                color="primary"
                class="create-place"
              >
                {{ t('charge.cost.createPlace') }}
              </v-btn>
            </div>
            <ChargeCostEditor
              v-if="settings && currency && !noCosts"
              :vehicle
              :charge
              :currency
              :limits="settings.limits.entered_cost"
              class="mt-4"
            />
          </v-card-text>
        </v-card>
      </v-col>

      <v-col cols="12" md="6">
        <v-card class="h-100 position">
          <v-card-title tag="h2" class="text-title-medium">{{ t('charge.position') }}</v-card-title>
          <v-card-text>
            <dl class="facts">
              <FactItem
                v-if="charge.address"
                :label="t('charge.address')"
                :value="charge.address"
              />
              <FactItem :label="t('charge.coordinates')" :value="f.coordinates(charge.position)" />
            </dl>
            <MapView
              v-if="charge.position"
              :label="t('charge.map')"
              :markers="[
                {
                  position: charge.position,
                  label: charge.place?.name ?? charge.address ?? t('charge.position'),
                },
              ]"
              class="mt-4"
            />
            <v-btn
              v-if="charge.position && !mapsOn"
              :href="openStreetMapUrl(charge.position)"
              target="_blank"
              rel="noreferrer"
              variant="tonal"
              color="primary"
              :append-icon="mdiOpenInNew"
              class="mt-4"
            >
              {{ t('common.openInOsm') }}
            </v-btn>
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
