<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import { useI18n } from 'vue-i18n'
import { vehicleLabel } from '@/entities/vehicle'
import { useVehicle } from '@/features/select-vehicle'
import { isApiError } from '@/shared/api'
import { AppShell } from '@/widgets/app-shell'
import { VehicleState } from '@/widgets/vehicle-state'
import { extensionVehicleActions } from '../extensions'

// The page is the vehicle's state now, with what it did over the last day or week: the
// totals of longer periods are the statistics'. The chart comes on demand, with Chart.js:
// the page itself, the first one opened, shows the state without waiting for it.
const SocTimeline = defineAsyncComponent(() =>
  import('@/widgets/soc-timeline').then((m) => m.SocTimeline),
)

const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const { vehicle: current, error } = useVehicle(() => props.vehicle)
const label = computed(() => (current.value ? vehicleLabel(current.value) : null))
</script>

<template>
  <AppShell :vehicle="vehicle">
    <template v-if="isApiError(error, 'not_found')">
      <v-alert type="error" variant="tonal" class="mb-4">{{ t('vehicle.notFound') }}</v-alert>
      <v-btn :to="{ name: 'home' }" variant="tonal" color="primary">{{ t('vehicle.home') }}</v-btn>
    </template>
    <template v-else>
      <!-- The bar's menu already names the vehicle, and its settings give the VIN: the
           heading stays for the page's outline and a screen reader, unseen. -->
      <h1 class="d-sr-only">{{ label ?? t('vehicle.title') }}</h1>
      <VehicleState :vehicle="vehicle">
        <template #aside>
          <SocTimeline :vehicle="vehicle" class="h-100" />
        </template>
      </VehicleState>
      <component
        :is="action"
        v-for="(action, i) in extensionVehicleActions"
        :key="i"
        :vehicle="vehicle"
        class="mt-4"
      />
    </template>
  </AppShell>
</template>
