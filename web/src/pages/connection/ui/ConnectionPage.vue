<script setup lang="ts">
import { mdiArrowLeft } from '@mdi/js'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { limitsPage } from '@/entities/session'
import { vehicleLabels } from '@/entities/vehicle'
import { ApiKeyForm, useConnectionSettings } from '@/features/manage-api-key'
import { useLimits } from '@/features/view-limits'
import { useCollections } from '@/features/watch-collection'
import { useFormat, useNow } from '@/shared/lib'
import { FactItem } from '@/shared/ui'
import { AppShell } from '@/widgets/app-shell'
import VehicleCollection from './VehicleCollection.vue'

// ConnectionPage is the hidden part: the Volvo ID that lets Runsten read, the application
// key the vehicles are read with, and how the collector reads each vehicle. The other
// pages only warn when nothing new is read; this one says why, and how to connect again.
// The vehicle it was opened from, if any, is kept for the way back. The Volvo ID flow
// comes back to it with its outcome (volvo=connected, key_refused, no_vehicle, which
// names the button to click again, or too_many_vehicles, which leads to the offer when
// the instance has a page of it), told once: the parameter leaves the URL, a reload does
// not tell it again.
//
// Without the instance's key (the hosted offer), the account's comes first: the Volvo ID
// is connected after, the vehicles listed with it. With it (self-hosting), it reads every
// vehicle: there is no key to give.
const props = defineProps<{ vehicle?: string; outcome?: string }>()
const { t } = useI18n()
const { unread } = useLimits()
const route = useRoute()
const router = useRouter()
const outcomes = ['connected', 'key_refused', 'no_vehicle', 'too_many_vehicles'] as const
type Outcome = (typeof outcomes)[number]
const outcome = ref(outcomes.find((o) => o === props.outcome))
const outcomeText = (o: Outcome) => {
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
onMounted(() => {
  if (route.query.volvo !== undefined) {
    void router.replace({ query: { ...route.query, volvo: undefined } })
  }
})
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
// One Volvo ID per account: every vehicle carries the same connection.
const connection = computed(() => vehicles.value?.[0]?.connection)
const lost = computed(() => connection.value?.status === 'reauth_required')
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
      <section v-if="!settings.instance_key" class="mb-8" aria-labelledby="connection-key-title">
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
                : settings.connected
                  ? t('connection.volvoId.noVehicle')
                  : t('connection.volvoId.none')
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

      <section v-if="settings.instance_key" class="mb-8" aria-labelledby="connection-app-title">
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

      <section v-if="vehicles.length" aria-labelledby="connection-vehicles-title">
        <h2 id="connection-vehicles-title" class="text-title-large mb-2">
          {{ t('connection.vehicles.title') }}
        </h2>
        <p class="text-body-medium mb-4">{{ t('connection.vehicles.help') }}</p>
        <v-alert v-if="error" type="warning" variant="tonal" density="compact" class="mb-4">
          {{ t('connection.refreshFailed') }}
        </v-alert>
        <VehicleCollection
          v-for="v in vehicles"
          :key="v.id"
          :vehicle="v"
          :label="labels.get(v.id) ?? v.vin"
          :now
          :unread="unread(v.id)"
        />
      </section>
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
