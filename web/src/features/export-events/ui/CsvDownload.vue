<script setup lang="ts">
import { mdiDownload } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { chargesCsvPath } from '@/entities/charge'
import { tripsCsvPath } from '@/entities/trip'

// CsvDownload downloads the trips or charges of the list's period as a CSV file, for a
// spreadsheet: a link, not a fetch, so that the browser saves the file as it comes.
const props = defineProps<{
  kind: 'trips' | 'charges'
  vehicle: string
  from?: string
  to?: string
}>()
const { t } = useI18n()
const href = computed(() => {
  const query = { from: props.from, to: props.to }
  return props.kind === 'trips'
    ? tripsCsvPath(props.vehicle, query)
    : chargesCsvPath(props.vehicle, query)
})
const label = computed(() => (props.kind === 'trips' ? t('trips.csv') : t('charges.csv')))
</script>

<template>
  <v-btn :href="href" download :prepend-icon="mdiDownload" variant="tonal" class="csv-download">
    {{ label }}
  </v-btn>
</template>
