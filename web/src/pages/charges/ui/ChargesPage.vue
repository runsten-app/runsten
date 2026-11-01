<script setup lang="ts">
import { defineAsyncComponent } from 'vue'
import { useI18n } from 'vue-i18n'
import { CsvDownload, ReadingsDownload } from '@/features/export-events'
import { PeriodPicker, usePeriod } from '@/features/filter-period'
import { PlaceSelect, usePlaceFilter } from '@/features/filter-place'
import { HistoryLimitNotice, useLimits } from '@/features/view-limits'
import { AppShell } from '@/widgets/app-shell'
import { PeriodTotals } from '@/widgets/period-totals'
import { ChargeList } from '@/widgets/charge-list'

defineProps<{ vehicle: string }>()
const { t } = useI18n()
const { days, period } = usePeriod()
const { place } = usePlaceFilter()

// The chart comes on demand, with Chart.js: the list shows without waiting for it.
const PeriodChart = defineAsyncComponent(() =>
  import('@/widgets/period-chart').then((m) => m.PeriodChart),
)
const { lacks } = useLimits()
</script>

<template>
  <AppShell :vehicle="vehicle">
    <div class="d-flex flex-wrap align-center justify-space-between ga-3 mb-4">
      <h1 class="text-headline-small">{{ t('charges.title') }}</h1>
      <!-- Every value the vehicle reported is no list of its own: its file sits with
           the lists' ones. -->
      <div v-if="period.valid && !lacks('csv')" class="d-flex flex-wrap align-center ga-2">
        <CsvDownload kind="charges" :vehicle="vehicle" :from="period.from" :to="period.to" :place />
        <ReadingsDownload :vehicle="vehicle" :from="period.from" :to="period.to" />
      </div>
    </div>
    <HistoryLimitNotice />
    <!-- The scope, the period first, then the place. -->
    <div class="d-flex flex-wrap align-start ga-3 mb-4">
      <PeriodPicker />
      <PlaceSelect />
    </div>
    <!-- An invalid period is told by the picker, and never sent. -->
    <template v-if="period.valid">
      <PeriodTotals kind="charges" :vehicle="vehicle" :from="period.from" :to="period.to" :place />
      <PeriodChart
        kind="charges"
        :vehicle="vehicle"
        :from="period.from"
        :to="period.to"
        :days
        :place
      />
      <ChargeList :vehicle="vehicle" :from="period.from" :to="period.to" :place />
    </template>
  </AppShell>
</template>
