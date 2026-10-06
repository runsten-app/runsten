<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useTheme } from 'vuetify'
import { useLimits } from '@/features/view-limits'
import { useStats } from '@/features/view-stats'
import { lastDays, parsePeriod, useFormat } from '@/shared/lib'
import { TileFigure } from '@/shared/ui'
import { ChartFrame, type Series } from '@/shared/ui/chart'

// RecentSummary is the vehicle's last 30 days at a glance: the distance, the consumption
// and the energy charged, and the distance by day. Pressing a day leads to its trips; the
// link to the statistics opens the same days there. Nothing in those days: nothing shown,
// the state above says enough.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const f = useFormat()
const theme = useTheme()
const router = useRouter()
const { lacks } = useLimits()
const days = lastDays(30, new Date())
const period = parsePeriod(days.from, days.to)
const from = period.valid ? period.from : undefined
const to = period.valid ? period.to : undefined
// Where the offer leaves out the statistics by interval, the totals only.
const { stats } = useStats(
  () => props.vehicle,
  () => from,
  () => to,
  () => (lacks('stats') ? undefined : 'day'),
)

function color(name: string): string {
  const c = theme.current.value.colors[name]
  return typeof c === 'string' ? c : '#000'
}

const totals = computed(() => stats.value?.totals)
const shown = computed(
  () => !!totals.value && totals.value.trips.count + totals.value.charges.count > 0,
)
const tiles = computed(() => {
  const s = totals.value
  if (!s) return []
  return [
    {
      key: 'distance',
      label: t('stats.tile.distance'),
      value: f.quantity(s.trips.distance_km, 'km'),
    },
    {
      key: 'consumption',
      label: t('stats.tile.consumption'),
      value: f.quantity(s.trips.consumption_kwh_per_100km, 'kWhPer100km'),
    },
    {
      key: 'energy',
      label: t('stats.tile.energy'),
      value: f.quantity(s.charges.energy_soc_kwh, 'kWh', 0),
    },
  ]
})

const intervals = computed(() => stats.value?.buckets ?? [])
const labels = computed(() => intervals.value.map((b) => f.bucket(b.start, 'day')))
const distance = computed(() => {
  const data = intervals.value.map((b) => b.trips.distance_km)
  const series: Series[] = [
    { label: t('stats.chart.distance.series'), data, color: color('chart-1') },
  ]
  let at = ''
  let max = 0
  data.forEach((v, i) => {
    if (v !== null && v > max) [max, at] = [v, labels.value[i] ?? '']
  })
  const summary = max
    ? t('stats.chart.distance.summary', {
        total: f.quantity(totals.value?.trips.distance_km ?? 0, 'km'),
        max: f.quantity(max, 'km'),
        at,
      })
    : t('stats.chart.distance.none')
  return { series, summary }
})

// open leads to the trips of a day.
function open(index: number) {
  const b = intervals.value[index]
  if (!b) return
  const day = f.dayKey(b.start)
  return router.push({
    name: 'trips',
    params: { vehicle: props.vehicle },
    query: { from: day, to: day },
  })
}
</script>

<template>
  <section v-if="shown" class="recent-summary" aria-labelledby="recent-summary-title">
    <div class="d-flex flex-wrap align-center justify-space-between ga-2 mb-3">
      <h2 id="recent-summary-title" class="text-title-large">{{ t('recent.title') }}</h2>
      <v-btn
        :to="{ name: 'stats', params: { vehicle }, query: days }"
        variant="text"
        color="primary"
        class="open-stats"
      >
        {{ t('recent.stats') }}
      </v-btn>
    </div>
    <!-- A <dl> holds div groups of dt and dd only. -->
    <dl class="tiles mb-3">
      <div v-for="tile in tiles" :key="tile.key" :class="['tile', tile.key]">
        <dt class="label">{{ tile.label }}</dt>
        <dd>
          <TileFigure :text="tile.value" :unknown="tile.value === f.unknown()" size="kpi" />
        </dd>
      </div>
    </dl>
    <ChartFrame
      v-if="intervals.length > 1"
      class="distance"
      kind="bar"
      :title="t('stats.chart.distance.title')"
      :summary="distance.summary"
      :hint="t('recent.hint')"
      :labels
      :series="distance.series"
      unit="km"
      :interval-header="t('stats.bucket.dayHeader')"
      :format="(v: number | null) => f.quantity(v, 'km')"
      @select="(i: number) => open(i)"
    />
  </section>
</template>

<style scoped>
.tiles {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}
.tile {
  padding: 20px;
  border-radius: var(--v-tile-radius);
  background: rgb(var(--v-theme-surface));
  min-width: 0;
}
.label {
  margin-bottom: 4px;
  font-size: 0.8125rem;
  color: rgb(var(--v-theme-text-secondary));
}
dd {
  margin: 0;
}
.distance :deep(.canvas) {
  height: 9rem;
}
@media (max-width: 599.98px) {
  .tiles {
    gap: 8px;
  }
  .tile {
    padding: 16px;
    border-radius: var(--v-tile-radius-phone);
  }
}
</style>
