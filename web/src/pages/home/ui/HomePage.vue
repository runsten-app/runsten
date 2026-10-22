<script setup lang="ts">
import { mdiCarOff } from '@mdi/js'
import { computed, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useConnectionSettings } from '@/features/manage-api-key'
import { useVehicles } from '@/features/select-vehicle'
import { AppShell } from '@/widgets/app-shell'

// Home leads to the first vehicle of the account; without one, it asks which car, Volvo or
// Polestar, two choices of equal weight. A Volvo leads to the Volvo ID connection, or first
// to the Connection page while the account's key is missing: without it, runsten-api
// refuses to start the flow (an instance without a key of its own). A Polestar leads to the
// Connection page's Polestar section, which takes its credential.
const { t } = useI18n()
const router = useRouter()
const { vehicles, isPending, error } = useVehicles()
const { connection } = useConnectionSettings()
const keyMissing = computed(
  () => !!connection.value && !connection.value.instance_key && !connection.value.api_key,
)

watchEffect(() => {
  const first = vehicles.value?.[0]
  if (first) void router.replace({ name: 'vehicle', params: { vehicle: first.id } })
})
</script>

<template>
  <AppShell>
    <v-alert v-if="error" type="error" variant="tonal">{{ t('home.error') }}</v-alert>
    <v-progress-linear v-else-if="isPending || vehicles?.length" indeterminate />
    <v-empty-state
      v-else
      :icon="mdiCarOff"
      :headline="t('home.empty.title')"
      :text="keyMissing ? t('home.empty.keyText') : t('home.empty.text')"
    >
      <template #actions>
        <v-btn
          v-if="keyMissing"
          :to="{ name: 'connection' }"
          color="primary"
          variant="flat"
          class="volvo"
          >{{ t('home.choose.volvo') }}</v-btn
        >
        <!-- A full page load: the Volvo ID flow is served by runsten-api. -->
        <v-btn v-else href="auth/volvo/start" color="primary" variant="flat" class="volvo">{{
          t('home.choose.volvo')
        }}</v-btn>
        <v-btn
          :to="{ name: 'connection', hash: '#polestar' }"
          color="primary"
          variant="flat"
          class="polestar"
          >{{ t('home.choose.polestar') }}</v-btn
        >
      </template>
    </v-empty-state>
  </AppShell>
</template>
