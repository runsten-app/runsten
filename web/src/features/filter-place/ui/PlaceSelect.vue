<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { usePlaceFilter } from '../composables/usePlaceFilter'
import { usePlaces } from '../composables/usePlaces'

// PlaceSelect narrows the charges to one group of the statistics' charges by place:
// one of the account's places, by name, outside every place, or without a position. A
// place since deleted, from an old link, is named so: its list is empty.
const { t, locale } = useI18n()
const { place, setPlace } = usePlaceFilter()
const { places } = usePlaces()

// The select's value for every charge: Vuetify shows no item for an empty one.
const any = 'any'
const items = computed(() => {
  const named = [...(places.value ?? [])]
    .sort((a, b) => a.name.localeCompare(b.name, locale.value))
    .map((p) => ({ title: p.name, value: p.id }))
  const known = !place.value || !places.value || ['outside', 'no_position'].includes(place.value)
  const deleted =
    known || named.some((p) => p.value === place.value)
      ? []
      : [{ title: t('placeFilter.deleted'), value: place.value ?? '' }]
  return [
    { title: t('placeFilter.any'), value: any },
    ...named,
    ...deleted,
    { title: t('placeFilter.outside'), value: 'outside' },
    { title: t('placeFilter.noPosition'), value: 'no_position' },
  ]
})
const value = computed({
  get: () => place.value ?? any,
  set: (v: string) => void setPlace(v === any ? undefined : v),
})
</script>

<template>
  <v-select
    v-model="value"
    :items
    :label="t('placeFilter.label')"
    class="place-select"
    variant="solo"
    bg-color="surface"
    density="compact"
    flat
    hide-details
  />
</template>

<style scoped>
.place-select {
  min-inline-size: 12rem;
  max-inline-size: 20rem;
}
</style>
