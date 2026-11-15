<script setup lang="ts">
import { mdiArrowLeft, mdiPrinterOutline } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { usePeriod } from '@/features/filter-period'
import { purposeQuery } from '@/features/mark-trip-purpose'
import { TripReport } from '@/widgets/trip-report'

// TripReportPage prints a period's trips: no shell, so that the page prints as it shows,
// and its buttons stay off the paper.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const route = useRoute()
// The period's days in the URL, as the list's; the API takes their instants.
const { days, period } = usePeriod()
const purpose = computed(() => purposeQuery(route.query))
const list = computed(() => ({
  name: 'trips',
  params: { vehicle: props.vehicle },
  query: { ...days.value, purpose: purpose.value },
}))
const print = () => window.print()
</script>

<template>
  <v-main>
    <v-container class="report-page">
      <div class="no-print d-flex flex-wrap align-center ga-2 mb-4">
        <v-btn :to="list" :prepend-icon="mdiArrowLeft" variant="text" class="back px-2">
          {{ t('trip.back') }}
        </v-btn>
        <v-spacer />
        <v-btn :prepend-icon="mdiPrinterOutline" color="primary" class="print" @click="print">
          {{ t('report.print') }}
        </v-btn>
      </div>
      <TripReport v-if="period.valid" :vehicle :from="period.from" :to="period.to" :purpose />
      <v-alert v-else type="error" variant="tonal" class="invalid-period">
        {{ t('period.error.rejected') }}
      </v-alert>
    </v-container>
  </v-main>
</template>

<style scoped>
.report-page {
  max-inline-size: 64rem;
}
@media print {
  .no-print {
    display: none !important;
  }
}
</style>
