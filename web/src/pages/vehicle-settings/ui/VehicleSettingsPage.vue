<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { vehicleLabel, VehicleSheet } from '@/entities/vehicle'
import { DeleteVehicle } from '@/features/delete-vehicle'
import { VehicleModelForm } from '@/features/edit-vehicle-model'
import { ImportList, ImportSources } from '@/features/manage-imports'
import { useVehicle } from '@/features/select-vehicle'
import { isApiError } from '@/shared/api'
import { SubNav } from '@/shared/ui'
import { AppShell } from '@/widgets/app-shell'

// VehicleSettingsPage is one vehicle's, in two areas, each a route: the vehicle (the variant
// and onboard charger the energies and costs rest on, its sheet, then its deletion, last),
// and the
// history imported from other applications (what it holds, then how to add to it). What
// the account shares (currency, places, tariffs) stays in the settings.
const props = defineProps<{ vehicle: string; area: 'model' | 'history' }>()
const { t } = useI18n()
const { vehicle: current, isPending, error } = useVehicle(() => props.vehicle)
const label = computed(() => (current.value ? vehicleLabel(current.value) : ''))
const areas = computed(() => [
  {
    to: { name: 'vehicleSettings', params: { vehicle: props.vehicle } },
    label: t('vehicleSettings.model.title'),
  },
  {
    to: { name: 'vehicleHistory', params: { vehicle: props.vehicle } },
    label: t('vehicleSettings.history.title'),
  },
])
const titleId = computed(() => `vehicle-settings-${props.area}-title`)
</script>

<template>
  <AppShell :vehicle="vehicle">
    <h1 class="text-headline-small mb-4">{{ t('vehicleSettings.title') }}</h1>
    <div class="areas">
      <SubNav :label="t('vehicleSettings.areas')" :items="areas" class="area-nav" />
      <section class="area" :aria-labelledby="titleId">
        <h2 :id="titleId" class="text-title-large mt-0 mb-1">
          {{
            area === 'model' ? t('vehicleSettings.model.title') : t('vehicleSettings.history.title')
          }}
        </h2>
        <p class="text-body-medium text-medium-emphasis mb-4">
          {{
            area === 'model' ? t('vehicleSettings.model.help') : t('vehicleSettings.history.help')
          }}
        </p>
        <v-alert v-if="isApiError(error, 'not_found')" type="error" variant="tonal">
          {{ t('vehicle.notFound') }}
        </v-alert>
        <v-alert v-else-if="error" type="error" variant="tonal">
          {{ t('vehicleSettings.error') }}
        </v-alert>
        <v-progress-linear
          v-else-if="isPending"
          indeterminate
          :aria-label="t('vehicleSettings.loading')"
        />
        <div v-else-if="current && area === 'model'" class="tiles">
          <v-card>
            <v-card-text>
              <VehicleModelForm :vehicle="current" :label />
            </v-card-text>
          </v-card>
          <!-- What the vehicle's source tells of it beyond its model; nothing when it tells none. -->
          <VehicleSheet :vehicle="current" />
          <!-- Destructive, so last, on a tile of its own. -->
          <DeleteVehicle :vehicle :label />
        </div>
        <div v-else-if="current" class="tiles">
          <ImportList :vehicle />
          <!-- The Volvo Data Portal's files are a Volvo's history: none of a Polestar. -->
          <ImportSources :vehicle :volvo="current.connection.provider === 'volvo'" />
        </div>
      </section>
    </div>
  </AppShell>
</template>

<style scoped>
/* The areas beside the content from md up, as the account's settings; above it on a phone. */
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
.tiles {
  display: grid;
  gap: 16px;
}
</style>
