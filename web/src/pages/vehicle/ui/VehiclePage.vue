<script setup lang="ts">
import { mdiCarCog } from '@mdi/js'
import { computed, defineAsyncComponent } from 'vue'
import { useI18n } from 'vue-i18n'
import { useDisplay } from 'vuetify'
import { vehicleLabel } from '@/entities/vehicle'
import { useVehicle } from '@/features/select-vehicle'
import { isApiError } from '@/shared/api'
import { AppShell } from '@/widgets/app-shell'
import { VehicleState } from '@/widgets/vehicle-state'

// The charts come on demand, with Chart.js: the page itself, the first one opened,
// shows the state without waiting for them.
const SocTimeline = defineAsyncComponent(() =>
  import('@/widgets/soc-timeline').then((m) => m.SocTimeline),
)
const RecentSummary = defineAsyncComponent(() =>
  import('@/widgets/recent-summary').then((m) => m.RecentSummary),
)

const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const { xs } = useDisplay()
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
      <div class="d-flex align-start justify-space-between ga-3 mb-2">
        <h1 class="text-headline-small vin title">{{ label ?? t('vehicle.title') }}</h1>
        <!-- Its label beyond a phone; on one, the icon with the same name. -->
        <v-btn
          :to="{ name: 'vehicleSettings', params: { vehicle } }"
          :icon="xs ? mdiCarCog : undefined"
          :prepend-icon="xs ? undefined : mdiCarCog"
          :text="xs ? undefined : t('vehicleSettings.open')"
          :aria-label="xs ? t('vehicleSettings.open') : undefined"
          :title="xs ? t('vehicleSettings.open') : undefined"
          color="surface"
          class="vehicle-settings"
        />
      </div>
      <VehicleState :vehicle="vehicle">
        <!-- The label names the model; the VIN still tells which car it is. -->
        <span v-if="current && label !== current.vin" class="vin">{{ current.vin }}</span>
      </VehicleState>
      <SocTimeline :vehicle="vehicle" class="mt-4" />
      <RecentSummary :vehicle="vehicle" class="mt-6" />
    </template>
  </AppShell>
</template>

<style scoped>
.vin {
  overflow-wrap: anywhere;
}
.title {
  min-inline-size: 0;
}
span.vin {
  letter-spacing: 0.03em;
}
</style>
