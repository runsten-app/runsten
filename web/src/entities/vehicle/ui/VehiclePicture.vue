<script setup lang="ts">
import { ref, watch } from 'vue'
import { vehiclePicturePath } from '../api/vehicleApi'
import type { Vehicle } from '../model/Vehicle'

// VehiclePicture shows the vendor's picture of the vehicle, which the API relays: nothing
// for a vehicle without one, or when it fails to load. Decorative: the vehicle's label
// beside it names it. small fits a line of text (a menu), large a tile.
const props = withDefaults(
  defineProps<{ vehicle: Pick<Vehicle, 'id' | 'sheet'>; size?: 'small' | 'large' }>(),
  { size: 'small' },
)
const failed = ref(false)
watch(
  () => props.vehicle.id,
  () => {
    failed.value = false
  },
)
</script>

<template>
  <img
    v-if="vehicle.sheet?.picture && !failed"
    :src="vehiclePicturePath(vehicle.id)"
    alt=""
    loading="lazy"
    :class="['vehicle-picture', size]"
    @error="failed = true"
  />
</template>

<style scoped>
.vehicle-picture {
  display: block;
  object-fit: contain;
}
/* The pictures are wide, the car seen from three quarters on a transparent ground. */
.small {
  block-size: 1.75rem;
  inline-size: 3rem;
}
/* Volvo's are 16:9, the car in a band across the middle: a wider frame crops the empty
   ground above and below it, never the car (half of the height at most). */
.large {
  inline-size: 100%;
  max-inline-size: 20rem;
  aspect-ratio: 2.2;
  object-fit: cover;
}
</style>
