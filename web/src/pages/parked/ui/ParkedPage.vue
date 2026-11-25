<script setup lang="ts">
import { computed, ref, watch, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { PeriodPicker, usePeriod } from '@/features/filter-period'
import { FeatureUnavailable, HistoryLimitNotice, useLimits } from '@/features/view-limits'
import { useParked, useStats } from '@/features/view-stats'
import { shortcutDays, useFormat } from '@/shared/lib'
import { AppShell } from '@/widgets/app-shell'
import { ParkedStops } from '@/widgets/parked-stops'

// ParkedPage is the vampire drain of a period: the loss while parked the statistics give,
// then each stop behind it, those of the rate only unless ?short=1 asks for all. Reached
// from the statistics' tile, whose name, tab and period it takes; this month unless the
// URL says otherwise.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const f = useFormat()
const { days, period, setPeriod } = usePeriod()
const route = useRoute()
const router = useRouter()
const all = computed(() => route.query.short === '1')
// The switch shows the click at once: the URL it writes follows a moment later.
const allShown = ref(all.value)
watch(all, (v) => (allShown.value = v))
const setAll = (v: boolean) => {
  allShown.value = v
  return router.replace({ query: { ...route.query, short: v ? '1' : undefined } })
}
const { lacks } = useLimits()
const noStats = computed(() => lacks('stats'))
const chosen = computed(() => !!(days.value.from || days.value.to))
watchEffect(() => {
  if (!chosen.value && !noStats.value) setPeriod(shortcutDays('thisMonth', new Date()))
})
const { stats } = useStats(
  () => props.vehicle,
  () => (chosen.value && period.value.valid ? period.value.from : undefined),
  () => (chosen.value && period.value.valid ? period.value.to : undefined),
)
// The short stops, counted for the toggle beside the period: the table's own query.
const shown = computed(() => !noStats.value && chosen.value && period.value.valid)
const { parked } = useParked(
  () => props.vehicle,
  () => (period.value.valid ? period.value.from : undefined),
  () => (period.value.valid ? period.value.to : undefined),
  shown,
)
const shortCount = computed(() => parked.value?.items.filter((s) => !s.rated).length ?? 0)
const minStop = computed(() => {
  const s = parked.value?.min_stop_s ?? 0
  return f.totalDuration({ min: s, max: s })
})
// The rate as the tile gives it, with what it stands for.
const rate = computed(() => {
  const pk = stats.value?.totals.parked
  if (!pk || pk.soc_loss_pct_per_day === null) return ''
  const parts = [
    t('stats.tile.perDay', { value: f.quantity(pk.soc_loss_pct_per_day, 'percent', 1) }),
  ]
  if (pk.power_w !== null)
    parts.push(t('stats.tile.parkedPower', { value: f.quantity(pk.power_w, 'W') }))
  return parts.join(' · ')
})
</script>

<template>
  <AppShell :vehicle="vehicle">
    <h1 class="text-headline-small mb-3">{{ t('stats.tile.parked') }}</h1>
    <FeatureUnavailable v-if="noStats" feature="stats" />
    <template v-else>
      <HistoryLimitNotice />
      <!-- What the table shows is chosen with the period, beside it. -->
      <div class="d-flex flex-wrap align-center ga-3 mb-4">
        <PeriodPicker :clearable="false" />
        <!-- A switch: its state shows by itself, its label stays the same. -->
        <v-switch
          v-if="shown && shortCount"
          :model-value="allShown"
          :label="t('parked.includeShort', { n: shortCount, min: minStop }, shortCount)"
          color="primary"
          density="compact"
          hide-details
          inset
          class="toggle-short flex-grow-0"
          @update:model-value="setAll(!!$event)"
        />
      </div>
      <template v-if="chosen && period.valid">
        <p v-if="rate" class="rate text-body-large mb-4">{{ rate }}</p>
        <ParkedStops :vehicle="vehicle" :from="period.from" :to="period.to" :all />
      </template>
    </template>
  </AppShell>
</template>
