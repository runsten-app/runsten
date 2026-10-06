<script setup lang="ts">
import { mdiCarArrowRight } from '@mdi/js'
import { useI18n } from 'vue-i18n'
import { useVehicleState } from '@/features/watch-vehicle-state'
import type { Position } from '@/shared/lib'

// VehiclePosition fills a place's position with a vehicle's last one, if it has one.
const props = defineProps<{ vehicle: string; label: string }>()
defineEmits<{ pick: [position: Position] }>()
const { t } = useI18n()
const { state } = useVehicleState(() => props.vehicle)
</script>

<template>
  <v-btn
    v-if="state?.position"
    :prepend-icon="mdiCarArrowRight"
    variant="tonal"
    color="primary"
    class="vehicle-position"
    @click="state?.position && $emit('pick', state.position.value)"
  >
    {{ t('place.vehiclePosition', { vehicle: label }) }}
  </v-btn>
</template>
