<script setup lang="ts">
import { mdiArrowLeft } from '@mdi/js'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { limitsPage } from '@/entities/session'
import { collectionState, needsAttention, vehicleLabels, type Vehicle } from '@/entities/vehicle'
import { ApiKeyForm, useConnectionSettings } from '@/features/manage-api-key'
import { BmwForm } from '@/features/manage-bmw'
import { PleosForms } from '@/features/manage-pleos'
import { PolestarForm, renewalDue } from '@/features/manage-polestar'
import { PolestarIdForm } from '@/features/manage-polestar-id'
import { TeslaForm } from '@/features/manage-tesla'
import { useFlag } from '@/features/view-flags'
import { useLimits } from '@/features/view-limits'
import { useCollections } from '@/features/watch-collection'
import { useFormat, useNow } from '@/shared/lib'
import { FactItem } from '@/shared/ui'
import { AppShell } from '@/widgets/app-shell'
import { extensionBrandSections } from '../extensions'
import { brands, pleosProviders, type Brand } from '../model/brands'
import VehicleCollection from './VehicleCollection.vue'

// ConnectionPage is the hidden part: the Volvo ID that lets Runsten read, the application
// key the vehicles are read with, the credential of Polestar's Data Portal, the refresh
// token of Tesla's Owner API, and how the collector reads each vehicle. The other
// pages only warn when nothing new is read; this one says why, and how to connect again.
// The vehicle it was opened from, if any, is kept for the way back. The Volvo ID flow
// comes back to it with its outcome (volvo=connected, key_refused, no_vehicle, which
// names the button to click again, or too_many_vehicles, which leads to the offer when
// the instance has a page of it), and so may the sign-in of a brand an extension
// connects (tesla=connected): told once, in the brand's tab, the parameter leaves the URL,
// a reload does not tell it again.
//
// Without the instance's key (the hosted offer), the account's comes first: the Volvo ID
// is connected after, the vehicles listed with it. With it (self-hosting), it reads every
// vehicle: there is no key to give.
//
// One tab per brand, each with its connection and its vehicles: a brand added later is a
// tab more. The tab is the URL's hash (#volvo, #polestar, #tesla, #bmw: the home's choice of a
// brand opens its tab); without one, that of the vehicle the page was opened from, else the
// first brand the account has a vehicle of, else Volvo's. A dot marks a tab whose vehicles need the user,
// or whose credential is to be renewed (Polestar's).
// On an instance without a Volvo application, Volvo's tab is left out, unless the account
// has Volvos from before: it then says why they are not read. Tesla's is there only while
// the flag source.tesla is on for the account, unless it has Teslas from before: it then
// says why they are not read. Polestar's holds the Data Portal, and the Polestar ID while
// the flag source.polestar-id is on for the account (off by default: an unofficial API, the
// instance's choice), or the account has Polestars read with it from before. BMW's is there
// only while the flag source.bmw is on for the account (on by default), unless it has a
// connection or BMWs from before: it then says why they are not read.
// A brand's section an extension sets (extensionBrandSections) takes the place of the
// core's, and shows its tab whatever the above.
const props = defineProps<{ vehicle?: string }>()
const { t } = useI18n()
const { unread } = useLimits()
const route = useRoute()
const router = useRouter()
const outcomes = ['connected', 'key_refused', 'no_vehicle', 'too_many_vehicles'] as const
type Outcome = (typeof outcomes)[number]
// The brand whose sign-in came back, by its parameter, read whenever the query brings one
// (the page stays mounted through a navigation to itself). Volvo's key is the only one a
// sign-in may refuse. Its tab goes into the hash, unless the URL names one, so that a later
// navigation (to another vehicle's tab) decides again; that one forgets the outcome too.
const outcomeBrand = ref<Brand>()
const outcome = ref<Outcome>()
watch(
  () => brands.find((b) => route.query[b] !== undefined),
  (b) => {
    if (!b) return
    const value = route.query[b]
    outcomeBrand.value = b
    outcome.value = outcomes.find((o) => o === value && (o !== 'key_refused' || b === 'volvo'))
    const dropped = Object.fromEntries(brands.map((x) => [x, undefined]))
    void router.replace({ query: { ...route.query, ...dropped }, hash: route.hash || `#${b}` })
  },
  { immediate: true },
)
watch(
  () => props.vehicle,
  () => {
    // An outcome that comes with the new vehicle stays: its parameter is still in the query.
    if (!brands.some((b) => route.query[b] !== undefined)) outcome.value = undefined
  },
)
const outcomeText = (o: Outcome) => {
  if (outcomeBrand.value && outcomeBrand.value !== 'volvo') {
    const brand = brandLabels.value[outcomeBrand.value]
    switch (o) {
      case 'connected':
        return t('connection.outcome.brand.connected', { brand })
      case 'no_vehicle':
        return t('connection.outcome.brand.noVehicle', { brand })
      default:
        return t('connection.outcome.brand.tooManyVehicles', { brand })
    }
  }
  switch (o) {
    case 'connected':
      return t('connection.outcome.connected')
    case 'key_refused':
      return t('connection.outcome.keyRefused')
    case 'no_vehicle':
      return t('connection.outcome.noVehicle', { button: connectLabel.value })
    case 'too_many_vehicles':
      return t('connection.outcome.tooManyVehicles')
  }
}
const f = useFormat()
const now = useNow()
const { vehicles, isPending, error } = useCollections()
const {
  connection: settings,
  isPending: settingsPending,
  error: settingsError,
} = useConnectionSettings()
// Without a key it may be read with, a Volvo ID would read nothing: runsten-api refuses
// to start the flow, and the button waits for the key.
const keyMissing = computed(
  () => !!settings.value && !settings.value.instance_key && !settings.value.api_key,
)
const connectLabel = computed(() =>
  settings.value?.connected ? t('connection.volvoId.reconnect') : t('connection.volvoId.connect'),
)
const labels = computed(() => vehicleLabels(vehicles.value ?? []))
// One Volvo ID per account: every Volvo carries the same connection.
const connection = computed(
  () => vehicles.value?.find((v) => v.connection.provider === 'volvo')?.connection,
)
const lost = computed(() => connection.value?.status === 'reauth_required')

const brandLabels = computed<Record<Brand, string>>(() => ({
  volvo: t('connection.brand.volvo'),
  polestar: t('connection.brand.polestar'),
  tesla: t('connection.brand.tesla'),
  bmw: t('connection.brand.bmw'),
  pleos: t('connection.brand.pleos'),
}))
// A Polestar read with the Polestar ID is a Polestar's: one tab.
const brandOf = (p?: Vehicle['connection']['provider']) =>
  p === 'polestar_id'
    ? 'polestar'
    : p === 'tesla_fleet'
      ? 'tesla'
      : pleosProviders.includes(p as (typeof pleosProviders)[number])
        ? 'pleos'
        : p
const ofBrand = (b: Brand) =>
  (vehicles.value ?? []).filter((v) => brandOf(v.connection.provider) === b)
const teslaOn = useFlag('source.tesla')
// The Owner API's flag off tells its own Teslas they are no longer read; those of the
// Fleet API are read by another source, which the flag does not stop.
const ownerTeslas = computed(() =>
  (vehicles.value ?? []).some((v) => v.connection.provider === 'tesla'),
)
const bmwOn = useFlag('source.bmw')
const pleosOn = useFlag('source.pleos')
// The API answers the three brands whatever the flag, each null without a key: a key
// given, not the object, keeps the tab while the flag is off.
const pleosKey = computed(() => Object.values(settings.value?.pleos ?? {}).some((k) => k !== null))
const polestarIdOn = useFlag('source.polestar-id')
const polestarIdShown = computed(
  () =>
    polestarIdOn.value ||
    !!settings.value?.polestar_id ||
    (vehicles.value ?? []).some((v) => v.connection.provider === 'polestar_id'),
)
const shown = computed(() =>
  brands.filter(
    (b) =>
      !!extensionBrandSections[b] ||
      (b === 'volvo'
        ? settings.value?.volvo !== false || ofBrand(b).length
        : b === 'tesla'
          ? teslaOn.value || ofBrand(b).length
          : b === 'pleos'
            ? pleosOn.value || pleosKey.value || ofBrand(b).length
            : b !== 'bmw' || bmwOn.value || !!settings.value?.bmw || ofBrand(b).length),
  ),
)
const asBrand = (s?: string) => shown.value.find((b) => b === s)
const tab = computed<Brand>({
  get() {
    const fromHash = asBrand(route.hash.slice(1))
    if (fromHash) return fromHash
    // A sign-in's parameter, until it leaves the URL for the hash.
    const signedIn = asBrand(brands.find((x) => route.query[x] !== undefined))
    if (signedIn) return signedIn
    const from = vehicles.value?.find((v) => v.id === props.vehicle)
    return (
      asBrand(brandOf(from?.connection.provider)) ??
      shown.value.find((b) => ofBrand(b).length) ??
      shown.value[0] ??
      'polestar'
    )
  },
  set(b) {
    void router.replace({ query: route.query, hash: `#${b}` })
  },
})
// The states the user is warned of, the vehicles the account's limits leave unread aside,
// as the user menu's dot.
const attention = (v: Vehicle) => {
  const s = collectionState(v, now.value, unread(v.id))
  return s.kind !== 'unread' && needsAttention(s)
}
const alerted = computed(
  () =>
    new Set(
      brands.filter(
        (b) =>
          ofBrand(b).some(attention) ||
          (b === 'polestar' && renewalDue(settings.value?.polestar, now.value)),
      ),
    ),
)
</script>

<template>
  <AppShell>
    <v-btn
      v-if="props.vehicle"
      :to="{ name: 'vehicle', params: { vehicle: props.vehicle } }"
      :active="false"
      :prepend-icon="mdiArrowLeft"
      variant="text"
      class="back mb-2 px-2"
    >
      {{ t('connection.backToVehicle') }}
    </v-btn>
    <h1 class="text-headline-small mb-2">{{ t('connection.title') }}</h1>
    <p class="text-body-medium mb-6">{{ t('connection.intro') }}</p>

    <v-alert
      v-if="outcome"
      :type="outcome === 'connected' ? 'success' : 'warning'"
      variant="tonal"
      closable
      class="mb-6"
      @click:close="outcome = undefined"
    >
      {{ outcomeText(outcome) }}
      <v-btn
        v-if="outcome === 'too_many_vehicles' && limitsPage"
        :to="limitsPage"
        variant="outlined"
        color="warning"
        size="small"
        class="lift mt-2 d-block"
      >
        {{ t('limits.lift') }}
      </v-btn>
    </v-alert>

    <v-alert
      v-if="(error && !vehicles) || (settingsError && !settings)"
      type="error"
      variant="tonal"
    >
      {{ t('connection.error') }}
    </v-alert>
    <v-progress-linear
      v-else-if="isPending || settingsPending"
      indeterminate
      :aria-label="t('connection.loading')"
    />
    <template v-else-if="vehicles && settings">
      <v-tabs
        v-model="tab"
        :aria-label="t('connection.brands')"
        slider-color="primary"
        class="brands mb-6"
      >
        <v-tab
          v-for="b in shown"
          :id="`connection-tab-${b}`"
          :key="b"
          :value="b"
          :aria-controls="`connection-panel-${b}`"
          class="brand"
        >
          {{ brandLabels[b] }}
          <v-chip
            v-if="b === 'pleos'"
            size="x-small"
            variant="tonal"
            color="primary"
            class="ms-2 beta"
          >
            {{ t('connection.polestar.beta') }}
          </v-chip>
          <template v-if="alerted.has(b)">
            <v-badge dot inline color="warning" class="attention ms-1" aria-hidden="true" />
            <!-- A space before: the tab's name would run into it. -->
            <span class="d-sr-only">{{ ` ${t('connection.brandAttention')}` }}</span>
          </template>
        </v-tab>
      </v-tabs>

      <v-tabs-window v-model="tab">
        <v-tabs-window-item
          v-for="b in shown"
          :id="`connection-panel-${b}`"
          :key="b"
          :value="b"
          role="tabpanel"
          :aria-labelledby="`connection-tab-${b}`"
        >
          <component :is="extensionBrandSections[b]" v-if="extensionBrandSections[b]" />
          <section
            v-else-if="b === 'volvo' && !settings.volvo"
            class="mb-8"
            aria-labelledby="connection-no-volvo-title"
          >
            <h2 id="connection-no-volvo-title" class="text-title-large mb-3">
              {{ t('connection.noVolvo.title') }}
            </h2>
            <p class="text-body-medium">{{ t('connection.noVolvo.text') }}</p>
          </section>
          <template v-else-if="b === 'volvo'">
            <section
              v-if="!settings.instance_key"
              class="mb-8"
              aria-labelledby="connection-key-title"
            >
              <h2 id="connection-key-title" class="text-title-large mb-3">
                {{ t('connection.key.title') }}
              </h2>
              <ApiKeyForm :connection="settings" />
            </section>

            <section class="mb-8" aria-labelledby="connection-volvo-title">
              <h2 id="connection-volvo-title" class="text-title-large mb-3">
                {{ t('connection.volvoId.title') }}
              </h2>
              <template v-if="!connection">
                <p class="text-body-medium mb-3">
                  {{
                    keyMissing
                      ? t('connection.volvoId.keyFirst')
                      : !settings.connected
                        ? t('connection.volvoId.none')
                        : settings.api_key?.refused_at
                          ? t('connection.volvoId.noVehicle')
                          : t('connection.volvoId.empty', { button: connectLabel })
                  }}
                </p>
                <!-- A full page load: the Volvo ID flow is served by runsten-api. -->
                <v-btn v-if="!keyMissing" href="auth/volvo/start" color="primary" variant="flat">
                  {{ connectLabel }}
                </v-btn>
              </template>
              <template v-else>
                <dl class="facts mb-4">
                  <FactItem
                    :label="t('connection.volvoId.status')"
                    :value="lost ? t('connection.volvoId.reauth') : t('connection.volvoId.active')"
                  />
                  <FactItem
                    :label="t('connection.volvoId.authorizedAt')"
                    :value="
                      connection.authorized_at
                        ? f.dateTime(connection.authorized_at, now)
                        : t('connection.volvoId.pasted')
                    "
                  />
                  <FactItem
                    v-if="connection.refreshed_at"
                    :label="t('connection.volvoId.refreshedAt')"
                    :value="f.dateTime(connection.refreshed_at, now)"
                  />
                  <FactItem
                    v-if="connection.reauth_at"
                    :label="t('connection.volvoId.lostAt')"
                    :value="f.dateTime(connection.reauth_at, now)"
                  />
                  <FactItem
                    v-if="connection.reauth_reason"
                    :label="t('connection.volvoId.reason')"
                    :value="connection.reauth_reason"
                  />
                </dl>
                <v-btn
                  href="auth/volvo/start"
                  :color="lost ? 'warning' : 'primary'"
                  :variant="lost ? 'flat' : 'tonal'"
                >
                  {{ t('connection.volvoId.reconnect') }}
                </v-btn>
                <p class="text-body-medium mt-2">{{ t('connection.volvoId.reconnectHelp') }}</p>
              </template>
            </section>

            <section
              v-if="settings.instance_key"
              class="mb-8"
              aria-labelledby="connection-app-title"
            >
              <h2 id="connection-app-title" class="text-title-large mb-3">
                {{ t('connection.app.title') }}
              </h2>
              <p class="text-body-medium mb-3">{{ t('connection.app.help') }}</p>
              <dl class="facts">
                <FactItem :label="t('connection.app.clientId')" :value="settings.client_id" />
                <FactItem
                  :label="t('connection.app.key')"
                  :value="t('apiKey.last4', { last4: settings.instance_key.last4 })"
                />
              </dl>
            </section>
          </template>
          <template v-else-if="b === 'polestar'">
            <section class="mb-8" aria-labelledby="connection-polestar-title">
              <h2 id="connection-polestar-title" class="text-title-large mb-3">
                {{ t('connection.polestar.title') }}
              </h2>
              <PolestarForm :connection="settings" />
            </section>
            <section
              v-if="polestarIdShown"
              class="mb-8 polestar-id"
              aria-labelledby="connection-polestar-id-title"
            >
              <h2 id="connection-polestar-id-title" class="text-title-large mb-3">
                {{ t('connection.polestarId.title') }}
                <v-chip size="x-small" variant="tonal" color="primary" class="ms-2 beta">
                  {{ t('connection.polestar.beta') }}
                </v-chip>
              </h2>
              <PolestarIdForm v-if="polestarIdOn" :connection="settings" />
              <p v-else class="text-body-medium polestar-id-off">
                {{ t('connection.polestarId.off') }}
              </p>
            </section>
          </template>
          <section v-else-if="b === 'pleos'" class="mb-8" aria-labelledby="connection-pleos-title">
            <h2 id="connection-pleos-title" class="text-title-large mb-3">
              {{ t('connection.pleos.title') }}
              <v-chip size="x-small" variant="tonal" color="primary" class="ms-2 beta">
                {{ t('connection.polestar.beta') }}
              </v-chip>
            </h2>
            <p v-if="!pleosOn" class="text-body-medium pleos-off">
              {{ t('connection.pleos.off') }}
            </p>
            <!-- Off, a key given before stays told, and can be removed. -->
            <PleosForms v-if="pleosOn || pleosKey" :connection="settings" :off="!pleosOn" />
          </section>
          <section v-else-if="b === 'bmw'" class="mb-8" aria-labelledby="connection-bmw-title">
            <h2 id="connection-bmw-title" class="text-title-large mb-3">
              {{ t('connection.bmw.title') }}
            </h2>
            <BmwForm v-if="bmwOn" :connection="settings" />
            <p v-else class="text-body-medium bmw-off">{{ t('connection.bmw.off') }}</p>
          </section>
          <section v-else class="mb-8" aria-labelledby="connection-tesla-title">
            <h2 id="connection-tesla-title" class="text-title-large mb-3">
              {{ t('connection.tesla.title') }}
            </h2>
            <TeslaForm v-if="teslaOn" :connection="settings" />
            <p v-else-if="ownerTeslas" class="text-body-medium tesla-off">
              {{ t('connection.tesla.off') }}
            </p>
          </section>

          <section v-if="ofBrand(b).length" :aria-labelledby="`connection-vehicles-${b}`">
            <h2 :id="`connection-vehicles-${b}`" class="text-title-large mb-2">
              {{ t('connection.vehicles.title') }}
            </h2>
            <p class="text-body-medium mb-4">{{ t('connection.vehicles.help') }}</p>
            <v-alert v-if="error" type="warning" variant="tonal" density="compact" class="mb-4">
              {{ t('connection.refreshFailed') }}
            </v-alert>
            <VehicleCollection
              v-for="v in ofBrand(b)"
              :key="v.id"
              :vehicle="v"
              :label="labels.get(v.id) ?? v.vin"
              :now
              :unread="unread(v.id)"
            />
          </section>
        </v-tabs-window-item>
      </v-tabs-window>
    </template>
  </AppShell>
</template>

<style scoped>
.facts {
  display: grid;
  gap: 1rem;
  grid-template-columns: repeat(auto-fill, minmax(14rem, 1fr));
}
</style>
