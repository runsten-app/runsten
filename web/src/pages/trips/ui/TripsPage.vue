<script setup lang="ts">
import { defineAsyncComponent } from 'vue'
import { useI18n } from 'vue-i18n'
import { CsvDownload } from '@/features/export-events'
import { PeriodPicker, usePeriod } from '@/features/filter-period'
import { HistoryLimitNotice, useLimits } from '@/features/view-limits'
import { AppShell } from '@/widgets/app-shell'
import { PeriodTotals } from '@/widgets/period-totals'
import { TripList } from '@/widgets/trip-list'

defineProps<{ vehicle: string }>()
const { t } = useI18n()
const { days, period } = usePeriod()

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
      <CsvDownload
        v-if="period.valid && !lacks('csv')"
        kind="trips"
        :vehicle="vehicle"
        :from="period.from"
        :to="period.to"
      />
    </div>
    <HistoryLimitNotice />
    <PeriodPicker class="mb-4" />
    <!-- An invalid period is told by the picker, and never sent. -->
    <template v-if="period.valid">
      <PeriodTotals kind="trips" :vehicle="vehicle" :from="period.from" :to="period.to" />
      <PeriodChart kind="trips" :vehicle="vehicle" :from="period.from" :to="period.to" :days />
      <TripList :vehicle="vehicle" :from="period.from" :to="period.to" />
    </template>
  </AppShell>
</template>
