<script setup lang="ts">
import { mdiChevronDown } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { purposes, usePurposeLabel, type Mark, type Trip } from '@/entities/trip'
import { useMarkTrips } from '../composables/useMarkTrips'

// PurposeMenu shows a trip's purpose in a list, and changes it: a purpose, none, or back
// to the place's rule. Beside the trip's link, never inside it: a button in a link is
// out of reach of a keyboard.
const props = defineProps<{ vehicle: string; trip: Trip }>()
const { t } = useI18n()
const purposeLabel = usePurposeLabel()
const { markTrips, isPending, error } = useMarkTrips()

const label = computed(() => {
  const p = props.trip.purpose
  if (p) return purposeLabel(p)
  return props.trip.purpose_origin === 'hand' ? t('purpose.none') : t('purpose.mark')
})
const title = computed(() => {
  const place = props.trip.purpose_place
  if (props.trip.purpose_origin === 'rule' && place)
    return t('purpose.fromRule', { place: place.name })
  if (props.trip.purpose_origin === 'hand') return t('purpose.byHand')
  return t('purpose.mark')
})
// A mark by hand may go back to the rules; without a mark, none by hand says so.
const items = computed(() => {
  const out: { mark: Mark; title: string }[] = purposes.map((p) => ({
    mark: p,
    title: purposeLabel(p),
  }))
  if (props.trip.purpose_origin === 'hand')
    out.push({ mark: 'rule', title: t('purpose.backToRule') })
  else out.push({ mark: 'none', title: t('purpose.none') })
  return out
})

function mark(m: Mark) {
  void markTrips({ vehicle: props.vehicle, trips: [props.trip.id], mark: m }).catch(() => {})
}
</script>

<template>
  <v-menu>
    <template #activator="{ props: activator }">
      <v-btn
        v-bind="activator"
        :loading="isPending"
        :color="error ? 'error' : trip.purpose ? 'secondary' : undefined"
        :variant="trip.purpose ? 'tonal' : 'text'"
        :title
        :aria-label="t('purpose.of', { purpose: label })"
        size="small"
        :append-icon="mdiChevronDown"
        class="purpose-menu"
        :class="{ 'by-rule': trip.purpose_origin === 'rule' }"
      >
        {{ label }}
        <span v-if="trip.purpose_origin === 'rule'" class="auto ms-1 text-medium-emphasis">
          {{ t('purpose.auto') }}
        </span>
      </v-btn>
    </template>
    <v-list density="compact">
      <v-list-item
        v-for="i in items"
        :key="i.mark"
        :title="i.title"
        :active="i.mark === trip.purpose"
        @click="mark(i.mark)"
      />
    </v-list>
  </v-menu>
</template>

<style scoped>
.auto {
  font-size: 0.75em;
}
</style>
