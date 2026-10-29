<script setup lang="ts">
import { mdiOpenInNew } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ChargeInProgress } from '@/entities/charge'
import { collectorStoppedAfter } from '@/entities/vehicle'
import { useSettings } from '@/features/edit-settings'
import { FeatureUnavailable, useLimits } from '@/features/view-limits'
import { kilowatts, openStreetMapUrl, useFormat, useMapsOn, useNow } from '@/shared/lib'
import { FactItem, MapView, StatusPill } from '@/shared/ui'

// ChargeInProgressDetails is the charge in progress as it stands at its latest reading,
// read again every minute by the page: what it added so far, its power, its cost so far.
// Once the readings stop, it is never said to go on: only when it was last read.
const props = defineProps<{ charge: ChargeInProgress }>()
const { t } = useI18n()
const f = useFormat()
const mapsOn = useMapsOn()
const now = useNow()
const { settings } = useSettings()
const { lacks } = useLimits()
const noCosts = computed(() => lacks('costs'))

const stale = computed(() => now.value - Date.parse(props.charge.read_at) > collectorStoppedAfter)
const day = computed(() => f.dayKey(props.charge.start.after))
// So far: from its start to its latest reading charging, which a pause leaves behind.
const sofar = computed(() => ({
  after: props.charge.charging_at,
  before: props.charge.charging_at,
}))
const paused = computed(() => props.charge.charging_at !== props.charge.read_at)

const cost = computed(() => f.chargeCost(props.charge.cost))
const costNote = computed(() => {
  const c = props.charge
  if (c.cost) return t('chargeInProgress.costSoFar', { place: c.place?.name ?? '' })
  if (!settings.value) return null
  if (!settings.value.currency) return t('charge.cost.noCurrency')
  return t('charge.cost.unknown')
})
</script>

<template>
  <article class="charge-in-progress">
    <header class="mb-4">
      <h1 class="text-headline-small">{{ t('chargeInProgress.title') }}</h1>
      <p class="text-body-large text-medium-emphasis">{{ f.day(charge.start.after) }}</p>
    </header>

    <v-alert v-if="stale" type="warning" variant="tonal" class="stopped mb-4">
      {{ t('chargeInProgress.stoppedHelp', { time: f.dateTime(charge.read_at, now) }) }}
    </v-alert>

    <v-row>
      <v-col cols="12" md="6">
        <v-card class="h-100 when">
          <v-card-title tag="h2" class="text-title-medium">{{ t('event.when') }}</v-card-title>
          <v-card-text>
            <StatusPill :tone="stale ? 'warning' : 'success'" class="state mb-3">
              {{
                stale
                  ? t('chargeInProgress.stopped')
                  : paused
                    ? t('chargeInProgress.paused')
                    : t('chargeInProgress.label')
              }}
            </StatusPill>
            <dl class="facts">
              <FactItem :label="t('event.start')" :value="f.bounds(charge.start, day)" />
              <FactItem
                :label="t('chargeInProgress.charging')"
                :value="f.duration(charge.start, sofar)"
              />
              <FactItem
                :label="t('chargeInProgress.readAt')"
                :value="`${f.time(charge.read_at, day)} (${f.age(charge.read_at, now)})`"
              />
            </dl>
            <p class="note text-body-small text-medium-emphasis mt-3">
              {{ t('chargeInProgress.note') }}
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
                :value="f.change(charge.start_soc_pct, charge.soc_pct, 'percent')"
              />
              <FactItem
                :label="t('charge.target')"
                :value="f.quantity(charge.target_soc_pct, 'percent')"
              />
              <FactItem
                :label="t('chargeInProgress.power')"
                :value="
                  f.quantity(charge.power_w === null ? null : kilowatts(charge.power_w), 'kW')
                "
              />
            </dl>
          </v-card-text>
        </v-card>
      </v-col>

      <v-col cols="12" md="6">
        <v-card class="h-100 energies">
          <v-card-title tag="h2" class="text-title-medium">{{
            t('chargeInProgress.energy')
          }}</v-card-title>
          <v-card-text>
            <dl class="facts">
              <FactItem
                v-if="charge.energy_measured_kwh !== null"
                :label="t('charge.energyMeasured')"
                :value="f.quantity(charge.energy_measured_kwh, 'kWh')"
              />
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
              {{
                charge.energy_measured_kwh !== null
                  ? t('charge.energyMeasuredNote')
                  : t('charge.energyNote')
              }}
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
                  v-if="charge.place"
                  :to="{ name: 'place', params: { place: charge.place.id } }"
                  class="place-link d-block text-body-medium text-primary mt-1"
                >
                  {{ t('charge.cost.openPlace') }}
                </router-link>
              </FactItem>
              <FactItem
                v-if="!noCosts"
                :label="t('chargeInProgress.cost')"
                :value="cost.text"
                :spoken="cost.spoken"
              >
                <span
                  v-if="costNote"
                  class="cost-note d-block text-body-medium text-medium-emphasis mt-1"
                >
                  {{ costNote }}
                </span>
              </FactItem>
            </dl>
            <FeatureUnavailable v-if="noCosts" feature="costs" class="mt-4" />
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
