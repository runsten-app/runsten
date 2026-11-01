<script setup lang="ts">
import { mdiDownload } from '@mdi/js'
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { readingsCsvPath } from '@/entities/vehicle'
import { readingsPeriod } from '../model/readingsWindow'

// ReadingsDownload downloads every value the vehicle reported within the list's period,
// as a CSV file: a link, saved by the browser as it comes. The API takes a month at
// most: beyond it, or for a period open at an end, the button is disabled and says why.
const props = defineProps<{ vehicle: string; from?: string; to?: string }>()
const { t } = useI18n()
const hint = useId()
const href = computed(() => {
  const period = readingsPeriod(props.from, props.to)
  return period && readingsCsvPath(props.vehicle, period)
})
</script>

<template>
  <div class="d-flex flex-wrap align-center ga-2">
    <v-btn
      v-if="href"
      :href="href"
      download
      :prepend-icon="mdiDownload"
      variant="tonal"
      class="readings-download"
    >
      {{ t('readingsCsv.download') }}
    </v-btn>
    <template v-else>
      <v-btn
        disabled
        :prepend-icon="mdiDownload"
        variant="tonal"
        class="readings-download"
        :aria-describedby="hint"
      >
        {{ t('readingsCsv.download') }}
      </v-btn>
      <span :id="hint" class="text-body-small text-medium-emphasis">
        {{ t('readingsCsv.tooLong') }}
      </span>
    </template>
  </div>
</template>
