<script setup lang="ts">
import { mdiArrowRight } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useLimits } from '@/features/view-limits'
import { useStats } from '@/features/view-stats'
import { lastDays, parsePeriod, useFormat } from '@/shared/lib'
import { TileFigure } from '@/shared/ui'

// ParkedSummary tells on the health page the loss while parked of the last 30 days, as
// the statistics' tile gives it, and leads to its stops over the same days: what a car
// loses standing still is a matter of its health as much as of its statistics. An account
// without the statistics sees none of it.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const f = useFormat()
const { lacks } = useLimits()
const shown = computed(() => !lacks('stats'))
// The 30 days up to today, fixed when the page opens.
const days = lastDays(30, new Date())
const period = parsePeriod(days.from, days.to)
const { stats } = useStats(
  () => props.vehicle,
  () => (period.valid ? period.from : undefined),
  () => (period.valid ? period.to : undefined),
  undefined,
  undefined,
  shown,
)
const parked = computed(() => stats.value?.totals.parked ?? null)
const figure = computed(() => {
  const pk = parked.value
  if (!pk) return null
  if (pk.soc_loss_pct_per_day === null)
    return { value: t('parked.summary.none'), known: false, notes: [] }
  const notes: string[] = []
  if (pk.power_w !== null)
    notes.push(t('stats.tile.parkedPower', { value: f.quantity(pk.power_w, 'W') }))
  if (pk.asleep_share !== null)
    notes.push(
      t('stats.tile.parkedAsleep', { value: f.quantity(pk.asleep_share * 100, 'percent') }),
    )
  return {
    value: t('stats.tile.perDay', { value: f.quantity(pk.soc_loss_pct_per_day, 'percent', 1) }),
    known: true,
    notes,
  }
})
</script>

<template>
  <v-card v-if="shown" class="tile parked-summary">
    <v-card-title tag="h2">{{ t('stats.tile.parked') }}</v-card-title>
    <v-card-text>
      <dl v-if="figure">
        <div>
          <dt class="text-medium-emphasis">{{ t('parked.summary.window') }}</dt>
          <dd>
            <TileFigure
              class="value"
              :text="figure.value"
              :size="figure.known ? 'kpi' : 'small'"
              :unknown="!figure.known"
            />
            <span v-if="figure.notes.length" class="notes d-block text-medium-emphasis">{{
              figure.notes.join(' · ')
            }}</span>
          </dd>
        </div>
      </dl>
      <router-link
        :to="{ name: 'parked', params: { vehicle }, query: { from: days.from, to: days.to } }"
        class="open text-primary"
      >
        {{ t('stats.tile.parkedStops') }}
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
.notes {
  font-size: 0.8125rem;
  margin-top: 4px;
}
.open {
  display: inline-block;
  margin-top: 12px;
}
</style>
