<script setup lang="ts">
import { mdiCarCog, mdiCheck, mdiChevronDown } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, type RouteLocationRaw } from 'vue-router'
import { vehicleLabel, vehicleLabels } from '@/entities/vehicle'
import { useVehicle } from '../composables/useVehicle'
import { useVehicles } from '../composables/useVehicles'

// VehicleMenu names the vehicle in the bar, on every page once the account has one: the
// pages under it rely on it rather than repeating its label. It lists the account's
// vehicles, each leading to the same section of the other (a details page to its list, a
// list with its period), the account's pages to its state; then the vehicle's settings.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const route = useRoute()
const { vehicles, error } = useVehicles()

const sections: Record<string, string> = {
  vehicle: 'vehicle',
  health: 'health',
  battery: 'battery',
  trips: 'trips',
  trip: 'trips',
  charges: 'charges',
  charge: 'charges',
  stats: 'stats',
  vehicleSettings: 'vehicleSettings',
  vehicleHistory: 'vehicleHistory',
}
// The period of a list or the statistics, and the charges' place, the account's;
// nothing else of the query.
const periodic = new Set(['trips', 'charges', 'stats'])

function target(vehicle: string): RouteLocationRaw {
  const name = sections[String(route.name)] ?? 'vehicle'
  const { from, to, place } = route.query
  if (!periodic.has(name)) return { name, params: { vehicle }, query: {} }
  return {
    name,
    params: { vehicle },
    query: name === 'charges' ? { from, to, place } : { from, to },
  }
}

// In the order of the list.
const items = computed(() =>
  [...vehicleLabels(vehicles.value ?? [])].map(([id, title]) => ({ id, title, to: target(id) })),
)
const listed = computed(() => items.value.find((i) => i.id === props.vehicle)?.title)
// The list failed, or lacks it: the vehicle's own read (cached on its pages) still names
// it, so that the way to its settings stays.
const { vehicle: own } = useVehicle(
  () => props.vehicle,
  () => (!!vehicles.value || !!error.value) && !listed.value,
)
const label = computed(() => listed.value ?? (own.value ? vehicleLabel(own.value) : ''))
const name = computed(() => t('vehicle.menu', { label: label.value }))
</script>

<template>
  <v-menu v-if="label" location="bottom start">
    <template #activator="{ props: activator }">
      <v-btn
        v-bind="activator"
        :append-icon="mdiChevronDown"
        :aria-label="name"
        variant="text"
        class="vehicle-menu"
      >
        <span class="text-truncate">{{ label }}</span>
      </v-btn>
    </template>
    <!-- A navigation of links, a landmark of its own out of the page's: each row, the
         divider included, a listitem. -->
    <nav :aria-label="name">
      <v-list density="compact" class="vehicle-list">
        <div v-for="item in items" :key="item.id" role="listitem">
          <v-list-item
            :to="item.to"
            :title="item.title"
            :aria-current="item.id === vehicle ? 'true' : undefined"
            :active="false"
            class="vehicle-item"
          >
            <template v-if="item.id === vehicle" #append>
              <v-icon :icon="mdiCheck" size="small" />
            </template>
          </v-list-item>
        </div>
        <div role="listitem"><v-divider class="my-1" /></div>
        <div role="listitem">
          <v-list-item
            :to="{ name: 'vehicleSettings', params: { vehicle } }"
            :prepend-icon="mdiCarCog"
            :title="t('vehicleSettings.open')"
            class="vehicle-settings"
          />
        </div>
      </v-list>
    </nav>
  </v-menu>
</template>

<style scoped>
.vehicle-menu {
  max-inline-size: 100%;
  font-size: 1rem;
}
.vehicle-menu :deep(.v-btn__content) {
  min-inline-size: 0;
}
.vehicle-list {
  min-inline-size: 16rem;
}
</style>
