<script setup lang="ts">
import { mdiChevronDown } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { vehicleLabels } from '@/entities/vehicle'
import { useVehicles } from '../composables/useVehicles'

// VehicleSelect switches to another vehicle of the account; shown only when there is one.
// It stays on the same list (a details page leads to its list), with the same period.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const lists: Record<string, string> = {
  trips: 'trips',
  trip: 'trips',
  charges: 'charges',
  charge: 'charges',
}
const { vehicles } = useVehicles()

// In the order of the list.
const items = computed(() =>
  [...vehicleLabels(vehicles.value ?? [])].map(([value, title]) => ({ value, title })),
)

function select(vehicle: string) {
  if (vehicle === props.vehicle) return
  const list = lists[String(route.name)]
  const { from, to } = route.query
  void router.push(
    list
      ? { name: list, params: { vehicle }, query: { from, to } }
      : { name: 'vehicle', params: { vehicle } },
  )
}
</script>

<template>
  <v-select
    v-if="items.length > 1"
    :model-value="vehicle"
    :items="items"
    :label="t('vehicle.select')"
    :menu-icon="mdiChevronDown"
    density="compact"
    single-line
    hide-details
    class="vehicle-select"
    @update:model-value="select"
  />
</template>

<style scoped>
.vehicle-select {
  max-width: 26rem;
}
</style>
