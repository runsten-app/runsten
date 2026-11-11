<script setup lang="ts">
import { computed } from 'vue'
import type { Vehicle } from '@/entities/vehicle'
import {
  best,
  perHundred,
  rankedConsumption,
  type GarageVehicle,
} from '@/features/compare-vehicles'
import VehicleTile from './VehicleTile.vue'

// GarageTiles are the account's vehicles side by side, a tile each, in the order of the
// account. A vehicle hidden from the comparison keeps its tile, faded, with the button
// that brings it back; the best figures are those of the vehicles compared.
const props = defineProps<{
  vehicles: Vehicle[]
  labels: Map<string, string>
  colors: Map<string, string>
  garage: GarageVehicle[]
  hidden: string[]
}>()
defineEmits<{ toggle: [vehicle: string] }>()

const compared = computed(() => props.garage.filter((v) => !props.hidden.includes(v.id)))
const bestConsumption = computed(() =>
  best(compared.value.map((v) => (v.period ? rankedConsumption(v.period.totals.trips) : null))),
)
const bestCost = computed(() =>
  best(
    compared.value.map((v) => {
      const s = v.period?.totals
      return s ? (perHundred(s.trips, s.charges)?.max_minor ?? null) : null
    }),
  ),
)
const data = (id: string): GarageVehicle => props.garage.find((v) => v.id === id) ?? { id }
</script>

<template>
  <div class="garage-tiles">
    <VehicleTile
      v-for="v in vehicles"
      :key="v.id"
      :vehicle="v"
      :label="labels.get(v.id) ?? v.vin"
      :color="colors.get(v.id) ?? ''"
      :data="data(v.id)"
      :hidden="hidden.includes(v.id)"
      :best-consumption="bestConsumption"
      :best-cost="bestCost"
      @toggle="$emit('toggle', v.id)"
    />
  </div>
</template>

<style scoped>
.garage-tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 12px;
}
@media (max-width: 599.98px) {
  .garage-tiles {
    gap: 8px;
  }
}
</style>
