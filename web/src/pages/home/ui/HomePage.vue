<script setup lang="ts">
import { mdiCarOff } from '@mdi/js'
import { computed, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useConnectionSettings } from '@/features/manage-api-key'
import { useFlag } from '@/features/view-flags'
import { useVehicles } from '@/features/select-vehicle'
import { AppShell } from '@/widgets/app-shell'

// Home leads to the first vehicle of the account; without one, it asks which car, Volvo,
// Polestar, Tesla or BMW, choices of equal weight, and says how Runsten reads each one
// offered, a part per brand joined as the language lists. A Volvo leads to the Volvo ID
// connection, or first to the Connection page while the account's key is missing: without
// it, runsten-api refuses to start the flow (an instance without a key of its own). A
// Polestar, a Tesla or a BMW leads to the Connection page's tab of its brand, which takes
// its credential, its refresh token or its client ID. On an instance without a Volvo
// application, the Volvo is left out; the Tesla, while the flag source.tesla is off for the
// account; the BMW, while source.bmw is.
const { t, locale } = useI18n()
const router = useRouter()
const { vehicles, isPending, error } = useVehicles()
const { connection } = useConnectionSettings()
const noVolvo = computed(() => connection.value?.volvo === false)
const teslaOn = useFlag('source.tesla')
const bmwOn = useFlag('source.bmw')
const keyMissing = computed(
  () => !!connection.value && !connection.value.instance_key && !connection.value.api_key,
)
const text = computed(() => {
  const makes = [
    ...(noVolvo.value ? [] : [keyMissing.value ? t('home.empty.volvoKey') : t('home.empty.volvo')]),
    t('home.empty.polestar'),
    ...(teslaOn.value ? [t('home.empty.tesla')] : []),
    ...(bmwOn.value ? [t('home.empty.bmw')] : []),
  ]
  const list = new Intl.ListFormat(locale.value, { type: 'conjunction' }).format(makes)
  const parts = [t('home.empty.ask'), t('home.empty.reads', { makes: list })]
  if (noVolvo.value) parts.push(t('home.empty.noVolvo'))
  return parts.join(' ')
})

watchEffect(() => {
  const first = vehicles.value?.[0]
  if (first) void router.replace({ name: 'vehicle', params: { vehicle: first.id } })
})
</script>

<template>
  <AppShell>
    <v-alert v-if="error" type="error" variant="tonal">{{ t('home.error') }}</v-alert>
    <v-progress-linear v-else-if="isPending || vehicles?.length" indeterminate />
    <v-empty-state v-else :icon="mdiCarOff" :headline="t('home.empty.title')" :text>
      <template #actions>
        <v-btn
          v-if="keyMissing && !noVolvo"
          :to="{ name: 'connection' }"
          color="primary"
          variant="flat"
          class="volvo"
          >{{ t('home.choose.volvo') }}</v-btn
        >
        <!-- A full page load: the Volvo ID flow is served by runsten-api. -->
        <v-btn
          v-else-if="!noVolvo"
          href="auth/volvo/start"
          color="primary"
          variant="flat"
          class="volvo"
          >{{ t('home.choose.volvo') }}</v-btn
        >
        <v-btn
          :to="{ name: 'connection', hash: '#polestar' }"
          color="primary"
          variant="flat"
          class="polestar"
          >{{ t('home.choose.polestar') }}</v-btn
        >
        <v-btn
          v-if="teslaOn"
          :to="{ name: 'connection', hash: '#tesla' }"
          color="primary"
          variant="flat"
          class="tesla"
          >{{ t('home.choose.tesla') }}</v-btn
        >
        <v-btn
          v-if="bmwOn"
          :to="{ name: 'connection', hash: '#bmw' }"
          color="primary"
          variant="flat"
          class="bmw"
          >{{ t('home.choose.bmw') }}</v-btn
        >
      </template>
    </v-empty-state>
  </AppShell>
</template>
