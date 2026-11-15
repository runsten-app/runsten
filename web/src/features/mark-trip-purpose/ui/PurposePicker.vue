<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { purposes, usePurposeLabel, type Trip } from '@/entities/trip'
import { useMarkTrips } from '../composables/useMarkTrips'

// PurposePicker is a trip's purpose on its details: where it comes from, and the choice
// of another, which overrides the place's rule.
const props = defineProps<{ vehicle: string; trip: Trip }>()
const { t } = useI18n()
const label = usePurposeLabel()
const { markTrips, isPending, error } = useMarkTrips()

const none = 'none'
const value = computed(() => props.trip.purpose ?? (props.trip.purpose_origin ? none : undefined))
const help = computed(() => {
  const tr = props.trip
  if (tr.purpose_origin === 'rule' && tr.purpose && tr.purpose_place)
    return t('purpose.help.rule', {
      place: tr.purpose_place.name,
      purpose: label(tr.purpose),
    })
  if (tr.purpose_origin === 'hand') return t('purpose.help.hand')
  return t('purpose.help.none')
})

function choose(v: unknown) {
  if (typeof v !== 'string' || v === value.value) return
  void markTrips({
    vehicle: props.vehicle,
    trips: [props.trip.id],
    mark: v === none ? 'none' : (v as Trip['purpose'] & string),
  }).catch(() => {})
}
function backToRule() {
  void markTrips({ vehicle: props.vehicle, trips: [props.trip.id], mark: 'rule' }).catch(() => {})
}
</script>

<template>
  <v-card class="h-100 purpose-picker">
    <v-card-title tag="h2" class="text-title-medium">{{ t('purpose.title') }}</v-card-title>
    <v-card-text>
      <v-btn-toggle
        :model-value="value"
        :disabled="isPending"
        variant="outlined"
        density="comfortable"
        divided
        class="flex-wrap"
        :aria-label="t('purpose.title')"
        @update:model-value="choose"
      >
        <v-btn v-for="p in purposes" :key="p" :value="p">{{ label(p) }}</v-btn>
        <v-btn :value="none">{{ t('purpose.none') }}</v-btn>
      </v-btn-toggle>
      <p class="help text-body-small text-medium-emphasis mt-3">
        {{ help }}
        <v-btn
          v-if="trip.purpose_origin === 'hand'"
          variant="text"
          density="compact"
          color="primary"
          class="back-to-rule px-1"
          @click="backToRule"
        >
          {{ t('purpose.backToRule') }}
        </v-btn>
      </p>
      <v-alert v-if="error" type="error" variant="tonal" density="compact" class="mt-2">
        {{ t('purpose.error') }}
      </v-alert>
    </v-card-text>
  </v-card>
</template>
