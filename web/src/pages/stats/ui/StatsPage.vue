<script setup lang="ts">
import { computed, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { PeriodPicker, usePeriod } from '@/features/filter-period'
import { FeatureUnavailable, HistoryLimitNotice, useLimits } from '@/features/view-limits'
import { useSettings } from '@/features/edit-settings'
import { BucketToggle, useBucket, useStatsView, ViewToggle } from '@/features/view-stats'
import { shortcutDays } from '@/shared/lib'
import { AppShell } from '@/widgets/app-shell'
import { StatsCharts } from '@/widgets/stats-charts'
import { StatsDistributions } from '@/widgets/stats-distributions'
import { StatsTiles } from '@/widgets/stats-tiles'
import { costsUnavailable } from '../extensions'

// StatsPage is the totals and the charts of a period: this month by day unless the URL
// says otherwise. Where the account's offer leaves the statistics out, it says so. Below
// the totals, one view of the charts at a time, driving by default: all at once were too
// many to find one's way. The costs' view says why it has none, when it has none; where the
// offer leaves them out, the hosted offer's build may show its own component instead.
defineProps<{ vehicle: string }>()
const { t } = useI18n()
const { days, period, setPeriod } = usePeriod()
const { bucket } = useBucket(days)
const { lacks } = useLimits()
const noStats = computed(() => lacks('stats'))
const { view } = useStatsView()
const { settings } = useSettings()
const noCosts = computed(() => lacks('costs'))
const noCurrency = computed(() => !noCosts.value && !!settings.value && !settings.value.currency)

// Without a period, this month: an open period would read the whole history.
const chosen = computed(() => !!(days.value.from || days.value.to))
watchEffect(() => {
  if (!chosen.value && !noStats.value) setPeriod(shortcutDays('thisMonth', new Date()))
})
</script>

<template>
  <AppShell :vehicle="vehicle">
    <h1 class="text-headline-small mb-3">{{ t('stats.title') }}</h1>
    <FeatureUnavailable v-if="noStats" feature="stats" />
    <template v-else>
      <HistoryLimitNotice />
      <PeriodPicker :clearable="false" class="mb-4" />
      <!-- An invalid period is told by the picker, and never sent. -->
      <template v-if="chosen && period.valid">
        <StatsTiles :vehicle="vehicle" :from="period.from" :to="period.to" :bucket />
        <!-- The split changes the charts only: beside their views, not above the tiles. -->
        <div class="d-flex flex-wrap align-center justify-space-between ga-3 mt-4 mb-3">
          <ViewToggle />
          <BucketToggle :days />
        </div>
        <template v-if="view === 'costs' && noCosts">
          <component :is="costsUnavailable" v-if="costsUnavailable" />
          <FeatureUnavailable v-else feature="costs" />
        </template>
        <v-alert v-else-if="view === 'costs' && noCurrency" type="info" variant="tonal">
          {{ t('stats.view.noCurrency') }}
          <v-btn
            :to="{ name: 'settings' }"
            variant="outlined"
            color="info"
            size="small"
            class="choose-currency mt-2 d-block"
          >
            {{ t('charge.cost.chooseCurrency') }}
          </v-btn>
        </v-alert>
        <template v-else>
          <StatsCharts :vehicle="vehicle" :from="period.from" :to="period.to" :bucket :view />
          <StatsDistributions
            :vehicle="vehicle"
            :from="period.from"
            :to="period.to"
            :bucket
            :view
          />
        </template>
        <p class="scope text-body-small text-medium-emphasis mt-4 mb-0">{{ t('stats.scope') }}</p>
      </template>
    </template>
  </AppShell>
</template>
