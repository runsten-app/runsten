<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { purposes, usePurposeLabel, type PurposeFilter } from '@/entities/trip'
import { usePurposeFilter } from '../composables/usePurposeFilter'

// PurposeFilter narrows the trips to one purpose, or to those without one.
const { t } = useI18n()
const label = usePurposeLabel()
const { purpose, setPurpose } = usePurposeFilter()

const all = 'all'
const items = computed(() => [
  { value: all, title: t('purpose.all') },
  ...purposes.map((p) => ({ value: p, title: label(p) })),
  { value: 'none', title: label('none') },
])
const value = computed({
  get: () => purpose.value ?? all,
  set: (v: string) => void setPurpose(v === all ? undefined : (v as PurposeFilter)),
})
</script>

<template>
  <v-chip-group
    v-model="value"
    mandatory
    selected-class="purpose-filter-selected"
    class="purpose-filter"
    :aria-label="t('purpose.filter')"
  >
    <v-chip v-for="i in items" :key="i.value" :value="i.value" filter variant="outlined">
      {{ i.title }}
    </v-chip>
  </v-chip-group>
</template>
