<script setup lang="ts">
import { mdiArrowRight } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useBattery } from '@/features/view-battery'
import { useFormat } from '@/shared/lib'
import { TileFigure } from '@/shared/ui'
import { remainingOf } from '../model/remaining'
import { capacityTrend } from '../model/trend'
import CapacitySparkline from './CapacitySparkline.vue'

// BatterySummary tells on the health page the battery's estimated capacity, an estimate
// among the health's figures, its evolution as the battery's page words it, and its
// monthly medians as a line: the one figure of the health that tells a story over time.
// It leads to the battery's page: its estimates, its charts. Without an answer, the link
// alone: the health stays whole.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const f = useFormat()
const { battery } = useBattery(() => props.vehicle)

const figure = computed(() => {
  const b = battery.value
  if (!b) return null
  const remaining = remainingOf(b)
  if (remaining)
    return {
      label: t('battery.tile.remaining'),
      value: t('battery.tile.remainingValue', { pct: f.quantity(remaining.pct, 'percent') }),
      known: true,
    }
  if (b.current)
    return {
      label: t('battery.tile.capacity'),
      value: f.quantity(b.current.capacity_kwh, 'kWh'),
      known: true,
    }
  return { label: t('battery.tile.capacity'), value: t('battery.tile.notEnough'), known: false }
})

// The API's evolution, the battery page's: from the median of the first estimates, once
// there are 12 months of them.
const change = computed(() => {
  const c = battery.value?.change
  return c
    ? t('battery.tile.changeSince', {
        change: f.signedPercent(c.change_pct),
        date: f.calendarDay(c.since.slice(0, 10)),
      })
    : null
})

const trend = computed(() => (battery.value ? capacityTrend(battery.value) : null))
// Said as the battery page's chart says it: the first and last monthly medians.
const trendLabel = computed(() => {
  const tr = trend.value
  if (!tr) return ''
  return t('battery.chart.capacity.summary', {
    first: f.quantity(tr.first.kwh, 'kWh'),
    firstAt: f.bucket(tr.first.start, 'month'),
    last: f.quantity(tr.last.kwh, 'kWh'),
    lastAt: f.bucket(tr.last.start, 'month'),
  })
})
</script>

<template>
  <v-card class="tile battery-summary">
    <v-card-title tag="h2">{{ t('battery.title') }}</v-card-title>
    <v-card-text>
      <dl v-if="figure" class="mb-3">
        <div>
          <dt class="text-medium-emphasis">{{ figure.label }}</dt>
          <dd>
            <TileFigure
              class="value"
              :text="figure.value"
              :size="figure.known ? 'kpi' : 'small'"
              :unknown="!figure.known"
            />
          </dd>
          <dd v-if="change" class="change text-medium-emphasis">{{ change }}</dd>
        </div>
      </dl>
      <CapacitySparkline
        v-if="trend"
        class="mb-3"
        :trend="trend"
        :label="trendLabel"
        :from="f.bucket(trend.first.start, 'month')"
        :to="f.bucket(trend.last.start, 'month')"
      />
      <router-link :to="{ name: 'battery', params: { vehicle } }" class="text-primary">
        {{ t('battery.state.open') }}
        <v-icon :icon="mdiArrowRight" size="16" aria-hidden="true" />
      </router-link>
    </v-card-text>
  </v-card>
</template>

<style scoped>
dl,
dd {
  margin: 0;
}
dt {
  font-size: 0.8125rem;
  margin-bottom: 6px;
}
.change {
  font-size: 0.8125rem;
  margin-top: 2px;
}
</style>
