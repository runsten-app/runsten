<script setup lang="ts">
import { mdiCarOff } from '@mdi/js'
import { watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useVehicles } from '@/features/select-vehicle'
import { AppShell } from '@/widgets/app-shell'

// Home leads to the first vehicle of the account; without one, to the Volvo ID connection.
const { t } = useI18n()
const router = useRouter()
const { vehicles, isPending, error } = useVehicles()

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
      :text="t('home.empty.text')"
    >
      <template #actions>
        <!-- A full page load: the Volvo ID flow is served by runsten-api. -->
        <v-btn href="auth/volvo/start" color="primary" variant="flat">{{
          t('home.connectVolvo')
        }}</v-btn>
      </template>
    </v-empty-state>
  </AppShell>
</template>
