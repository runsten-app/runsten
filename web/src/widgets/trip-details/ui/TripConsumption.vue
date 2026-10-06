<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { bandOf } from '@/entities/stats'
import type { Trip } from '@/entities/trip'
import { useBandLabel, useStats } from '@/features/view-stats'
import { parsePeriod, shortcutDays, useFormat } from '@/shared/lib'
import { FactItem } from '@/shared/ui'

// TripConsumption sets a trip's consumption beside its month's: the average of every
// trip, and that of the trips of the same band of distance, as the statistics give them
// over the month the trip started in, in the reader's time zone.
const props = defineProps<{ vehicle: string; trip: Trip }>()
const { t } = useI18n()
const f = useFormat()
const bandLabel = useBandLabel()

const days = computed(() => shortcutDays('thisMonth', new Date(props.trip.start.after)))
const period = computed(() => parsePeriod(days.value.from, days.value.to))
const { stats } = useStats(
  () => props.vehicle,
  () => (period.value.valid ? period.value.from : undefined),
  () => (period.value.valid ? period.value.to : undefined),
)
// The month's name alone: the trip's day is in the page's heading.
const month = computed(() => f.month(new Date(props.trip.start.after).getMonth() + 1, 'long'))

// The trip's own, as the statistics count it: its energy over a positive distance.
const own = computed(() => {
  const { energy_kwh: e, distance_km: d } = props.trip
  return e === null || d === null || d <= 0 ? null : (e * 100) / d
})
// A reconstructed trip may hold several: no band holds it.
const band = computed(() =>
  props.trip.reconstructed || props.trip.distance_km === null || !stats.value
    ? undefined
    : bandOf(stats.value.trips_by_distance.bands, props.trip.distance_km),
)

// against is a reference, and how far the trip lies from it, when both are known.
function against(ref: number | null): { value: string; gap?: string } {
  const value = f.quantity(ref, 'kWhPer100km')
  if (ref === null || own.value === null || ref <= 0) return { value }
  const pct = Math.round((own.value / ref - 1) * 100)
  return { value, gap: t('trip.consumption.gap', { pct: f.signedPercent(pct) }) }
}
const average = computed(() => against(stats.value?.totals.trips.consumption_kwh_per_100km ?? null))
const similar = computed(() =>
  band.value ? against(band.value.consumption_kwh_per_100km) : undefined,
)
</script>

<template>
  <v-card class="h-100 consumption">
    <v-card-title tag="h2" class="text-title-medium">{{
      t('trip.consumption.title')
    }}</v-card-title>
    <v-card-text>
      <dl class="facts">
        <FactItem :label="t('trip.consumption.own')" :value="f.quantity(own, 'kWhPer100km')" />
        <template v-if="stats">
          <FactItem
            class="average"
            :label="t('trip.consumption.average', { month })"
            :value="average.value"
          >
            <span v-if="average.gap" class="gap d-block text-body-medium text-medium-emphasis">
              {{ average.gap }}
            </span>
          </FactItem>
          <FactItem
            v-if="band && similar"
            class="similar"
            :label="t('trip.consumption.similar', { band: bandLabel(band), month })"
            :value="similar.value"
          >
            <span v-if="similar.gap" class="gap d-block text-body-medium text-medium-emphasis">
              {{ similar.gap }}
            </span>
          </FactItem>
        </template>
      </dl>
      <p class="note text-body-small text-medium-emphasis mt-3">
        {{ t('trip.consumption.note') }}
      </p>
      <v-btn
        :to="{ name: 'stats', params: { vehicle }, query: days }"
        variant="text"
        color="primary"
        size="small"
        class="stats-link px-0 mt-1"
      >
        {{ t('trip.consumption.statsLink', { month }) }}
      </v-btn>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.facts {
  display: grid;
  gap: 1rem;
}
</style>
