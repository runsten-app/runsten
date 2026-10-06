<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { vehicleLabel } from '@/entities/vehicle'
import { VehicleModelForm } from '@/features/edit-vehicle-model'
import { useVehicle } from '@/features/select-vehicle'
import { isApiError } from '@/shared/api'
import { AppShell } from '@/widgets/app-shell'

// VehicleSettingsPage is one vehicle's: its variant and onboard charger. What the account
// shares (currency, places, tariffs) stays in the settings.
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
    <VehicleModelForm v-else-if="current" :vehicle="current" :label />
  </AppShell>
</template>
