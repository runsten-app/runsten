<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { vehicleLabel } from '@/entities/vehicle'
import { DeleteVehicle } from '@/features/delete-vehicle'
import { VehicleModelForm } from '@/features/edit-vehicle-model'
import { VehicleImports } from '@/features/manage-imports'
import { useVehicle } from '@/features/select-vehicle'
import { isApiError } from '@/shared/api'
import { AppShell } from '@/widgets/app-shell'

// VehicleSettingsPage is one vehicle's: its variant and onboard charger, the history
// imported from other applications, and its deletion. What the account shares (currency, places, tariffs)
// stays in the settings.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const { vehicle: current, isPending, error } = useVehicle(() => props.vehicle)
const label = computed(() => (current.value ? vehicleLabel(current.value) : ''))
</script>

<template>
  <AppShell :vehicle="vehicle">
    <h1 class="text-headline-small mb-2">{{ t('vehicleSettings.title') }}</h1>
    <p class="text-body-medium mb-6">{{ t('vehicleSettings.help') }}</p>
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
    <template v-else-if="current">
      <VehicleModelForm :vehicle="current" :label />
      <!-- The Volvo Data Portal's files are a Volvo's history: none of a Polestar. -->
      <VehicleImports :vehicle :volvo="current.connection.provider === 'volvo'" />
      <DeleteVehicle :vehicle :label />
    </template>
  </AppShell>
</template>
