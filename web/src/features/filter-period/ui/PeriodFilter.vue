<script setup lang="ts">
import { mdiClose } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { usePeriod } from '../composables/usePeriod'

// PeriodFilter picks the days of a list. Native date fields: the browser's own picker
// (a wheel on a phone), keyboard entry, and the YYYY-MM-DD the URL keeps. Without
// clearable, no button empties it: the statistics always have a period, their shortcuts
// lead back to one.
const { clearable = true } = defineProps<{ clearable?: boolean }>()
const { t } = useI18n()
const { days, period, setPeriod } = usePeriod()

const errors = computed(() => {
  const p = period.value
  if (p.valid) return { from: undefined, to: undefined }
  return {
    from: p.error === 'invalidFrom' ? t('period.error.invalidDate') : undefined,
    to:
      p.error === 'invalidTo'
        ? t('period.error.invalidDate')
        : p.error === 'inverted'
          ? t('period.error.inverted')
          : undefined,
  }
})
const filtered = computed(() => clearable && !!(days.value.from || days.value.to))
</script>

<template>
  <form class="period d-flex flex-wrap align-start ga-3" role="search" @submit.prevent>
    <v-text-field
      :model-value="days.from ?? ''"
      :label="t('period.from')"
      :error-messages="errors.from"
      :max="days.to"
      type="date"
      name="from"
      density="compact"
      class="date"
      @update:model-value="(v: string) => setPeriod({ ...days, from: v })"
    />
    <v-text-field
      :model-value="days.to ?? ''"
      :label="t('period.to')"
      :error-messages="errors.to"
      :min="days.from"
      type="date"
      name="to"
      density="compact"
      class="date"
      @update:model-value="(v: string) => setPeriod({ ...days, to: v })"
    />
    <v-btn
      v-if="filtered"
      :prepend-icon="mdiClose"
      variant="text"
      class="clear mt-1"
      @click="setPeriod({})"
    >
      {{ t('period.clear') }}
    </v-btn>
  </form>
</template>

<style scoped>
.date {
  flex: 1 1 10rem;
  max-width: 14rem;
}
</style>
