<script setup lang="ts">
import { mdiChevronRight } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { vehicleLabels } from '@/entities/vehicle'
import { PlaceList } from '@/features/edit-place'
import { CurrencyForm, useSettings } from '@/features/edit-settings'
import { AccountData } from '@/features/export-account'
import { AccessTokens } from '@/features/manage-access-tokens'
import { MqttBroker } from '@/features/manage-mqtt'
import { OrphanCosts } from '@/features/manage-orphan-costs'
import { useVehicles } from '@/features/select-vehicle'
import { FeatureUnavailable, useLimits } from '@/features/view-limits'
import { AppShell } from '@/widgets/app-shell'
import { extensionAccountActions, extensionSections } from '../extensions'

// SettingsPage is the account's, shared by its vehicles: its currency, its places with
// their tariffs, and the entered costs no charge takes any more, if any. What belongs to
// one vehicle (its model) is on that vehicle's settings page, which the list leads to.
// The entered costs are left out where the account's offer leaves the costs out. Then
// the access tokens programs read the API with, the MQTT broker the vehicles' state is
// published to (locked where the offer leaves it out), the sections an extension adds, and
// last, all the account's data, to download whatever the offer, with what an extension
// adds beside it.
const { t } = useI18n()
const { settings, isPending, error } = useSettings()
const { vehicles, isPending: vehiclesPending, error: vehiclesError } = useVehicles()
const labels = computed(() => vehicleLabels(vehicles.value ?? []))
const placeRoute = (place: string) => ({ name: 'place', params: { place } })
const { lacks } = useLimits()
</script>

<template>
  <AppShell>
    <h1 class="text-headline-small mb-4">{{ t('settings.title') }}</h1>

    <section class="mb-8" aria-labelledby="settings-currency-title">
      <h2 id="settings-currency-title" class="text-title-large mb-3">
        {{ t('settings.currency.title') }}
      </h2>
      <v-alert v-if="error" type="error" variant="tonal">{{ t('settings.error') }}</v-alert>
      <v-progress-linear v-else-if="isPending" indeterminate :aria-label="t('settings.loading')" />
      <CurrencyForm v-else-if="settings" :settings />
    </section>

    <section aria-labelledby="settings-places-title">
      <h2 id="settings-places-title" class="text-title-large mb-2">
        {{ t('settings.places.title') }}
      </h2>
      <p class="text-body-medium mb-4">{{ t('settings.places.help') }}</p>
      <!-- The list tells the limit of the places, from the settings: it waits for them. -->
      <v-alert v-if="error" type="error" variant="tonal">{{ t('settings.places.error') }}</v-alert>
      <v-progress-linear
        v-else-if="!settings"
        indeterminate
        :aria-label="t('settings.places.loading')"
      />
      <PlaceList
        v-else
        :currency="settings.currency"
        :max-places="settings.limits.places"
        :to="placeRoute"
      />
    </section>

    <section class="mt-8" aria-labelledby="settings-vehicles-title">
      <h2 id="settings-vehicles-title" class="text-title-large mb-2">
        {{ t('settings.vehicles.title') }}
      </h2>
      <p class="text-body-medium mb-4">{{ t('settings.vehicles.help') }}</p>
      <v-alert v-if="vehiclesError" type="error" variant="tonal">
        {{ t('settings.vehicles.error') }}
      </v-alert>
      <v-progress-linear
        v-else-if="vehiclesPending"
        indeterminate
        :aria-label="t('settings.vehicles.loading')"
      />
      <p v-else-if="!vehicles?.length" class="text-body-medium">
        {{ t('settings.vehicles.none') }}
      </p>
      <!-- A list of links: each one in a listitem, the divider inside it. -->
      <v-list v-else class="tile-list vehicle-links py-0">
        <div v-for="(v, i) in vehicles" :key="v.id" role="listitem">
          <v-divider v-if="i > 0" />
          <v-list-item
            :to="{ name: 'vehicleSettings', params: { vehicle: v.id } }"
            :append-icon="mdiChevronRight"
            class="vehicle-link py-3"
          >
            <v-list-item-title class="text-title-medium text-wrap">
              {{ labels.get(v.id) ?? v.vin }}
            </v-list-item-title>
            <p class="text-body-medium text-medium-emphasis">{{ v.vin }}</p>
          </v-list-item>
        </div>
      </v-list>
    </section>

    <OrphanCosts v-if="!lacks('costs')" />

    <section class="mt-8" aria-labelledby="settings-tokens-title">
      <h2 id="settings-tokens-title" class="text-title-large mb-2">
        {{ t('accessTokens.title') }}
      </h2>
      <AccessTokens />
    </section>

    <section class="mt-8" aria-labelledby="settings-mqtt-title">
      <h2 id="settings-mqtt-title" class="text-title-large mb-2">
        {{ t('mqtt.title') }}
      </h2>
      <FeatureUnavailable v-if="lacks('mqtt')" feature="mqtt" class="mb-4" />
      <MqttBroker :locked="lacks('mqtt')" />
    </section>

    <component :is="section" v-for="(section, i) in extensionSections" :key="i" />

    <section class="mt-8" aria-labelledby="settings-account-title">
      <h2 id="settings-account-title" class="text-title-large mb-2">
        {{ t('account.title') }}
      </h2>
      <AccountData>
        <component :is="action" v-for="(action, i) in extensionAccountActions" :key="i" />
      </AccountData>
    </section>
  </AppShell>
</template>
