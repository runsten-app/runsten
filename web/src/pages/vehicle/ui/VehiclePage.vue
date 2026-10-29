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
      <!-- The bar's picker already names the vehicle: the heading stays, for the page's
           outline and a screen reader, but small, its VIN beside it. -->
      <div class="heading d-flex flex-wrap align-center ga-3 mb-3">
        <div class="names d-flex flex-wrap align-baseline">
          <h1 class="text-title-large vin">{{ label ?? t('vehicle.title') }}</h1>
          <span v-if="current && label !== current.vin" class="vin text-medium-emphasis">{{
            current.vin
          }}</span>
        </div>
        <!-- Its label beyond a phone; on one, the icon with the same name. -->
        <v-btn
          :to="{ name: 'vehicleSettings', params: { vehicle } }"
          :icon="xs ? mdiCarCog : undefined"
          :prepend-icon="xs ? undefined : mdiCarCog"
          :text="xs ? undefined : t('vehicleSettings.open')"
          :aria-label="xs ? t('vehicleSettings.open') : undefined"
          :title="xs ? t('vehicleSettings.open') : undefined"
          :size="xs ? 'small' : undefined"
          variant="flat"
          color="surface"
          class="vehicle-settings ms-auto"
        />
      </div>
      <VehicleState :vehicle="vehicle">
        <template #aside>
          <SocTimeline :vehicle="vehicle" class="h-100" />
        </template>
      </VehicleState>
      <RecentSummary :vehicle="vehicle" class="mt-4" />
    </template>
  </AppShell>
</template>

<style scoped>
.names {
  gap: 4px 12px;
  min-inline-size: 0;
}
.vin {
  overflow-wrap: anywhere;
}
span.vin {
  font-size: 0.8125rem;
  letter-spacing: 0.03em;
}
</style>
