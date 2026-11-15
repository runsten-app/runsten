<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { purposes, usePurposeLabel, type Mark } from '@/entities/trip'
import { useMarkTrips } from '../composables/useMarkTrips'

// MarkSelectionBar marks the trips selected in the list at once.
const props = defineProps<{ vehicle: string; selected: string[] }>()
const emit = defineEmits<{ done: [] }>()
const { t } = useI18n()
const label = usePurposeLabel()
const { markTrips, isPending, error } = useMarkTrips()

async function mark(m: Mark) {
  try {
    await markTrips({ vehicle: props.vehicle, trips: props.selected, mark: m })
    emit('done')
  } catch {
    // Told below.
  }
}
</script>

<template>
  <v-sheet color="surface-variant" rounded="lg" class="mark-selection-bar pa-3 mb-3">
    <div class="d-flex flex-wrap align-center ga-2">
      <span class="text-body-medium me-auto" aria-live="polite">
        {{ t('purpose.selected', selected.length) }}
      </span>
      <v-btn
        v-for="p in purposes"
        :key="p"
        :disabled="!selected.length"
        :loading="isPending"
        variant="tonal"
        @click="mark(p)"
      >
        {{ label(p) }}
      </v-btn>
      <v-btn :disabled="!selected.length" :loading="isPending" variant="text" @click="mark('rule')">
        {{ t('purpose.clear') }}
      </v-btn>
    </div>
    <v-alert v-if="error" type="error" variant="tonal" density="compact" class="mt-2">
      {{ t('purpose.error') }}
    </v-alert>
  </v-sheet>
</template>
