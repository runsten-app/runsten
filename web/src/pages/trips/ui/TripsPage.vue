<script setup lang="ts">
import { mdiPrinterOutline } from '@mdi/js'
import { defineAsyncComponent, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { CsvDownload, ReadingsDownload } from '@/features/export-events'
import { periodQuery, PeriodPicker, usePeriod } from '@/features/filter-period'
import { MarkSelectionBar, PurposeFilter, usePurposeFilter } from '@/features/mark-trip-purpose'
import { HistoryLimitNotice, useLimits } from '@/features/view-limits'
import { AppShell } from '@/widgets/app-shell'
import { PeriodTotals } from '@/widgets/period-totals'
import { TripList } from '@/widgets/trip-list'
import { TripMap } from '@/widgets/trip-map'

const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const { days, period } = usePeriod()
const { purpose } = usePurposeFilter()

// Selecting marks several trips at once; another vehicle or filter starts it over.
const selecting = ref(false)
const selected = ref<string[]>([])
watch(
  [() => props.vehicle, () => JSON.stringify(period.value), purpose],
  () => (selected.value = []),
)
function toggleSelecting() {
  selecting.value = !selecting.value
  selected.value = []
}

// The chart comes on demand, with Chart.js: the list shows without waiting for it.
const PeriodChart = defineAsyncComponent(() =>
  import('@/widgets/period-chart').then((m) => m.PeriodChart),
)
const { lacks } = useLimits()
</script>

<template>
  <AppShell :vehicle="vehicle">
    <div class="d-flex flex-wrap align-center justify-space-between ga-3 mb-4">
      <h1 class="text-headline-small">{{ t('trips.title') }}</h1>
      <!-- Every value the vehicle reported is no list of its own: its file sits with
           the lists' ones. -->
      <div v-if="period.valid" class="d-flex flex-wrap align-center ga-2">
        <v-btn variant="tonal" class="select-trips" @click="toggleSelecting">
          {{ selecting ? t('purpose.done') : t('purpose.select') }}
        </v-btn>
        <v-btn
          :to="{
            name: 'trip-report',
            params: { vehicle },
            query: { ...periodQuery($route.query), purpose },
          }"
          :prepend-icon="mdiPrinterOutline"
          variant="tonal"
          class="trip-report-link"
        >
          {{ t('report.open') }}
        </v-btn>
        <template v-if="!lacks('csv')">
          <CsvDownload
            kind="trips"
            :vehicle="vehicle"
            :from="period.from"
            :to="period.to"
            :purpose
          />
          <ReadingsDownload :vehicle="vehicle" :from="period.from" :to="period.to" />
        </template>
      </div>
    </div>
    <HistoryLimitNotice />
    <PeriodPicker class="mb-4" />
    <!-- An invalid period is told by the picker, and never sent. -->
    <template v-if="period.valid">
      <PeriodTotals kind="trips" :vehicle="vehicle" :from="period.from" :to="period.to" />
      <PeriodChart kind="trips" :vehicle="vehicle" :from="period.from" :to="period.to" :days />
      <TripMap :vehicle="vehicle" :from="period.from" :to="period.to" />
      <PurposeFilter class="mb-2" />
      <MarkSelectionBar
        v-if="selecting"
        :vehicle="vehicle"
        :selected
        class="selection-bar"
        @done="selected = []"
      />
      <TripList
        v-model:selected="selected"
        :vehicle="vehicle"
        :from="period.from"
        :to="period.to"
        :purpose
        :selecting
      />
    </template>
  </AppShell>
</template>
