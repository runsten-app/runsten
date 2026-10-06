<script setup lang="ts">
import { mdiArrowLeft } from '@mdi/js'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import type { Place } from '@/entities/place'
import { vehicleLabels } from '@/entities/vehicle'
import { PlaceForm, usePlace } from '@/features/edit-place'
import { useSettings } from '@/features/edit-settings'
import { useVehicles } from '@/features/select-vehicle'
import { isApiError } from '@/shared/api'
import { AppShell } from '@/widgets/app-shell'
import VehiclePosition from './VehiclePosition.vue'

// PlacePage edits a place, or creates one (place undefined), at lat and lon when given.
const props = defineProps<{ place?: string; lat?: number; lon?: number }>()
const { t } = useI18n()
const router = useRouter()
const { place: current, error } = usePlace(() => props.place)
// The form checks the API's limits, from the settings: it waits for them.
const { settings, error: settingsError } = useSettings()
const { vehicles } = useVehicles()
const labels = computed(() => [...vehicleLabels(vehicles.value ?? [])])

const settingsRoute = { name: 'settings' }

// A new place, once saved, takes its ID in the URL: the same form stays, with its focus.
const created = ref<string>()
const formKey = computed(() => (props.place === created.value ? 'new' : (props.place ?? 'new')))
async function saved(p: Place) {
  if (props.place !== undefined) return
  created.value = p.id
  await router.replace({ name: 'place', params: { place: p.id }, query: {} })
}
const title = computed(() => current.value?.name ?? t('place.newTitle'))
</script>

<template>
  <AppShell>
    <v-btn
      :to="settingsRoute"
      :active="false"
      :prepend-icon="mdiArrowLeft"
      variant="text"
      class="back mb-2 px-2"
    >
      {{ t('place.back') }}
    </v-btn>
    <v-alert v-if="isApiError(error, 'not_found')" type="error" variant="tonal">
      {{ t('place.notFound') }}
    </v-alert>
    <v-alert v-else-if="error || settingsError" type="error" variant="tonal">
      {{ t('place.error.load') }}
    </v-alert>
    <v-progress-linear
      v-else-if="!settings || (place !== undefined && !current && !created)"
      indeterminate
      :aria-label="t('place.loading')"
    />
    <template v-else>
      <h1 class="text-headline-small mb-4 title">{{ title }}</h1>
      <PlaceForm
        :key="formKey"
        :place="current ?? null"
        :currency="settings.currency"
        :limits="settings.limits"
        :default-efficiency="settings.default_efficiency"
        :lat
        :lon
        @saved="saved"
        @deleted="router.push(settingsRoute)"
      >
        <template #position="{ setPosition }">
          <VehiclePosition
            v-for="[id, label] in labels"
            :key="id"
            :vehicle="id"
            :label
            @pick="(p) => setPosition(p, label)"
          />
        </template>
      </PlaceForm>
    </template>
  </AppShell>
</template>

<style scoped>
.title {
  overflow-wrap: anywhere;
}
</style>
