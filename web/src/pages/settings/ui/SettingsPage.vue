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
import { ReleaseInfo } from '@/features/view-release'
import { SubNav } from '@/shared/ui'
import { AppShell } from '@/widgets/app-shell'
import {
  extensionAccountActions,
  extensionAccountSections,
  extensionSections,
  releaseSection,
} from '../extensions'

// SettingsPage is the account's, shared by its vehicles, in four areas, each a route:
// - the costs: its currency, its places with their tariffs, and the entered costs no
//   charge takes any more, if any (left out where the account's offer leaves the costs
//   out);
// - the vehicles, each leading to its own settings (its model);
// - the integrations: the access tokens programs read the API with, the MQTT broker the
//   vehicles' state is published to (locked where the offer leaves it out), then the
//   sections an extension adds there;
// - the account: the sections an extension adds there, all its data, to download whatever
//   the offer, with what an extension adds beside it, and the release the instance runs,
//   unless the build leaves it out.
const props = defineProps<{ area: 'costs' | 'vehicles' | 'integrations' | 'account' }>()
const { t } = useI18n()
const { settings, isPending, error } = useSettings()
const { vehicles, isPending: vehiclesPending, error: vehiclesError } = useVehicles()
const labels = computed(() => vehicleLabels(vehicles.value ?? []))
const placeRoute = (place: string) => ({ name: 'place', params: { place } })
const { lacks } = useLimits()

const areas = computed(() => [
  { to: { name: 'settings' }, label: t('settings.costs.title') },
  { to: { name: 'settingsVehicles' }, label: t('settings.vehicles.title') },
  { to: { name: 'settingsIntegrations' }, label: t('settings.integrations.title') },
  { to: { name: 'settingsAccount' }, label: t('settings.account.title') },
])
const heading = computed(
  () =>
    ({
      costs: { title: t('settings.costs.title'), help: t('settings.costs.help') },
      vehicles: { title: t('settings.vehicles.title'), help: t('settings.vehicles.help') },
      integrations: {
        title: t('settings.integrations.title'),
        help: t('settings.integrations.help'),
      },
      account: { title: t('settings.account.title'), help: t('settings.account.help') },
    })[props.area],
)
const titleId = computed(() => `settings-area-${props.area}`)
</script>

<template>
  <AppShell>
    <h1 class="text-headline-small mb-4">{{ t('settings.title') }}</h1>
    <div class="areas">
      <SubNav :label="t('settings.areas')" :items="areas" class="area-nav" />
      <section class="area" :aria-labelledby="titleId">
        <h2 :id="titleId" class="text-title-large mt-0 mb-1">{{ heading.title }}</h2>
        <p class="text-body-medium text-medium-emphasis mb-6">{{ heading.help }}</p>

        <template v-if="area === 'costs'">
          <section class="part" aria-labelledby="settings-currency-title">
            <h3 id="settings-currency-title" class="text-title-medium mb-3">
              {{ t('settings.currency.title') }}
            </h3>
            <v-alert v-if="error" type="error" variant="tonal">{{ t('settings.error') }}</v-alert>
            <v-progress-linear
              v-else-if="isPending"
              indeterminate
              :aria-label="t('settings.loading')"
            />
            <CurrencyForm v-else-if="settings" :settings />
          </section>

          <section class="part" aria-labelledby="settings-places-title">
            <h3 id="settings-places-title" class="text-title-medium mb-1">
              {{ t('settings.places.title') }}
            </h3>
            <p class="text-body-medium text-medium-emphasis mb-3">
              {{ t('settings.places.help') }}
            </p>
            <!-- The list tells the limit of the places, from the settings: it waits for them. -->
            <v-alert v-if="error" type="error" variant="tonal">
              {{ t('settings.places.error') }}
            </v-alert>
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

          <OrphanCosts v-if="!lacks('costs')" class="part" />
        </template>

        <template v-else-if="area === 'vehicles'">
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
        </template>

        <template v-else-if="area === 'integrations'">
          <section class="part" aria-labelledby="settings-tokens-title">
            <h3 id="settings-tokens-title" class="text-title-medium mb-1">
              {{ t('accessTokens.title') }}
            </h3>
            <AccessTokens />
          </section>

          <section class="part" aria-labelledby="settings-mqtt-title">
            <h3 id="settings-mqtt-title" class="text-title-medium mb-1">
              {{ t('mqtt.title') }}
            </h3>
            <FeatureUnavailable v-if="lacks('mqtt')" feature="mqtt" class="mb-4" />
            <MqttBroker :locked="lacks('mqtt')" />
          </section>

          <component
            :is="section"
            v-for="(section, i) in extensionSections"
            :key="i"
            class="part"
          />
        </template>

        <template v-else>
          <component
            :is="section"
            v-for="(section, i) in extensionAccountSections"
            :key="i"
            class="part"
          />
          <section class="part" aria-labelledby="settings-account-title">
            <h3 id="settings-account-title" class="text-title-medium mb-1">
              {{ t('account.title') }}
            </h3>
            <AccountData>
              <component :is="action" v-for="(action, i) in extensionAccountActions" :key="i" />
            </AccountData>
          </section>

          <section v-if="releaseSection" class="part" aria-labelledby="settings-release-title">
            <h3 id="settings-release-title" class="text-title-medium mb-1">
              {{ t('release.title') }}
            </h3>
            <ReleaseInfo />
          </section>
        </template>
      </section>
    </div>
  </AppShell>
</template>

<style scoped>
/* The areas beside the content from md up, as a vehicle's settings; above it on a phone. */
.areas {
  display: grid;
  gap: 16px 32px;
}
@media (min-width: 960px) {
  .areas {
    grid-template-columns: 220px minmax(0, 1fr);
    align-items: start;
  }
}
.area {
  min-width: 0;
  max-width: 56rem;
}
/* The area's heading on the line of the first link beside it: their centres level. */
@media (min-width: 960px) {
  .area {
    padding-top: 6px;
  }
}
/* The parts of an area apart from one another, more than a part's own lines. */
.part + .part {
  margin-top: 40px;
}
</style>
