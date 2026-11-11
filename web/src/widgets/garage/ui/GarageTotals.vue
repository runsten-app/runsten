<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { isStale, vehicleMode, type State } from '@/entities/vehicle'
import { accountTotals, type GaragePeriod, type GarageVehicle } from '@/features/compare-vehicles'
import { useFormat, useNow } from '@/shared/lib'
import { TileFigure } from '@/shared/ui'

// GarageTotals are the account's figures over the period, every vehicle added up, hidden
// ones included: the distance, what the electricity cost and what 100 km cost on average;
// and, above them, how many vehicles drive or charge right now, as far as their state is
// current: a stale one may have stopped. They stand for the page's title: the Garage is
// the account's. A part unknown (a trip's distance, a charge's cost) is said under its
// figure; without a currency, no amount.
const props = defineProps<{
  garage: GarageVehicle[]
  period: GaragePeriod
}>()
const { t } = useI18n()
const f = useFormat()
const clock = useNow()
const current = (s: State) => !!s.checked_at && !isStale(s.checked_at, clock.value, s.connection)

const periods = computed(() => props.garage.flatMap((v) => (v.period ? [v.period] : [])))
const read = computed(() => periods.value.length === props.garage.length)
const totals = computed(() => accountTotals(periods.value.map((p) => p.totals)))
const currency = computed(() => periods.value.find((p) => p.currency)?.currency ?? null)

const now = computed(() => {
  const modes = props.garage.map((v) => (v.state && current(v.state) ? vehicleMode(v.state) : null))
  const driving = modes.filter((m) => m === 'driving').length
  const charging = modes.filter((m) => m === 'charging').length
  const parts = [
    ...(driving ? [t('garage.totals.driving', driving)] : []),
    ...(charging ? [t('garage.totals.charging', charging)] : []),
  ]
  return parts.length ? t('garage.totals.now', { what: f.list(parts) }) : null
})
const lead = computed(() =>
  [
    t('garage.totals.vehicles', props.garage.length),
    props.period === '30d' ? t('garage.period.last30d') : t('garage.period.last12m'),
    ...(now.value ? [now.value] : []),
  ].join(' · '),
)

const distance = computed(() => f.quantity(totals.value.distanceKm, 'km', 0))
const cost = computed(() =>
  totals.value.costKnown && currency.value
    ? f.amounts(totals.value.cost, currency.value)
    : { text: f.unknown(), spoken: f.unknown() },
)
const perHundred = computed(() =>
  currency.value && totals.value.perHundred
    ? f.amounts(totals.value.perHundred, currency.value)
    : { text: f.unknown(), spoken: f.unknown() },
)
</script>

<template>
  <section class="garage-totals mb-4" :aria-label="t('garage.totals.label')">
    <p class="lead text-body-medium text-medium-emphasis mb-2">{{ lead }}</p>
    <dl v-if="read" class="figures">
      <div class="figure distance">
        <dt class="label">{{ t('garage.totals.distance') }}</dt>
        <dd>
          <TileFigure :text="distance" size="kpi" />
          <span v-if="totals.distanceUnknown" class="note d-block">
            {{ t('stats.tile.distanceUnknown', totals.distanceUnknown) }}
          </span>
        </dd>
      </div>
      <template v-if="currency">
        <div class="figure cost">
          <dt class="label">{{ t('garage.totals.cost') }}</dt>
          <dd>
            <TileFigure
              :text="cost.text"
              :spoken="cost.spoken"
              :unknown="cost.text === f.unknown()"
              size="kpi"
            />
            <span v-if="totals.costUnknown && totals.costKnown" class="note d-block">
              {{ t('stats.tile.costUnknown', totals.costUnknown) }}
            </span>
          </dd>
        </div>
        <div class="figure per-hundred">
          <dt class="label">{{ t('garage.totals.perHundred') }}</dt>
          <dd>
            <TileFigure
              :text="perHundred.text"
              :spoken="perHundred.spoken"
              :unknown="perHundred.text === f.unknown()"
              size="kpi"
            />
          </dd>
        </div>
      </template>
    </dl>
  </section>
</template>

<style scoped>
.figures {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 40px;
  margin: 0;
}
.label {
  font-size: 0.8125rem;
  color: rgb(var(--v-theme-text-secondary));
}
dd {
  margin: 0;
}
.note {
  font-size: 0.75rem;
  color: rgb(var(--v-theme-text-secondary));
}
@media (max-width: 599.98px) {
  .figures {
    gap: 8px 24px;
  }
}
</style>
