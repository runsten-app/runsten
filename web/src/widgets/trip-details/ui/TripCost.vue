<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Trip } from '@/entities/trip'
import { useSettings } from '@/features/edit-settings'
import { useVehicle } from '@/features/select-vehicle'
import { FeatureUnavailable, useLimits } from '@/features/view-limits'
import { useFormat } from '@/shared/lib'
import { FactItem, SpokenText, TileFigure } from '@/shared/ui'

// TripCost is what a trip's energy cost: at the average price of the kWh that went into
// the battery over the 30 days before it, the charges it rests on, and where their
// energy was charged, each part with its price. Without the account's currency, a way
// to choose one; where the account's offer leaves the costs out, said so.
const props = defineProps<{ vehicle: string; trip: Trip }>()
const { t } = useI18n()
const f = useFormat()
const { settings } = useSettings()
const { lacks } = useLimits()
const noCosts = computed(() => lacks('costs'))
const currency = computed(() => settings.value?.currency ?? null)
// A plug-in hybrid's trip costs the electricity it used, not its fuel.
const { vehicle: details } = useVehicle(() => props.vehicle)
const hybrid = computed(() => details.value?.model.powertrain === 'plugin_hybrid')

const cost = computed(() => {
  const c = props.trip.cost
  if (!c) return null
  const per100 = c.per_100km && f.estimate(c.per_100km, c.currency)
  return {
    amount: f.estimate(c, c.currency),
    per100: per100 && {
      text: t('trip.cost.per100km', { amount: per100.text }),
      spoken: t('trip.cost.per100kmSpoken', { amount: per100.spoken }),
    },
    price: f.priceRange(c.price.price_per_kwh, c.currency),
    exact: c.min_minor === c.max_minor,
  }
})

// shares are where the energy of the charges was charged, each with its part and price.
const shares = computed(() => {
  const c = props.trip.cost
  if (!c) return []
  return c.price.shares.map((s, i) => {
    const pct = (s.energy_kwh / c.price.energy_kwh) * 100
    const name =
      s.place?.name ?? (s.type === 'DC' ? t('trip.cost.elsewhereDC') : t('trip.cost.elsewhereAC'))
    const price = f.priceRange(s.price_per_kwh, c.currency)
    return {
      key: s.place?.id ?? s.type ?? String(i),
      pct,
      color: `rgb(var(--v-theme-chart-${(i % 5) + 1}))`,
      text: t('trip.cost.share', { name, share: f.quantity(pct, 'percent', 0), price: price.text }),
      spoken: t('trip.cost.share', {
        name,
        share: f.quantity(pct, 'percent', 0),
        price: price.spoken,
      }),
    }
  })
})

const notes = computed(() => {
  const c = props.trip.cost
  if (!c) {
    return [props.trip.energy_kwh === null ? t('trip.cost.noEnergy') : t('trip.cost.noPrice')]
  }
  const out = [t('trip.cost.note')]
  if (c.min_minor !== c.max_minor) out.push(t('trip.cost.rangeNote'))
  if (c.price.left_out) out.push(t('trip.cost.leftOut', c.price.left_out))
  if (hybrid.value) out.push(t('trip.cost.hybrid'))
  return out
})
</script>

<template>
  <v-card v-if="settings" class="h-100 trip-cost">
    <v-card-title tag="h2" class="text-title-medium">{{ t('trip.cost.title') }}</v-card-title>
    <v-card-text>
      <FeatureUnavailable v-if="noCosts" feature="costs" />
      <template v-else-if="!currency">
        <p class="no-currency text-body-medium">{{ t('charge.cost.noCurrency') }}</p>
        <v-btn
          :to="{ name: 'settings' }"
          variant="tonal"
          color="primary"
          class="choose-currency mt-3"
        >
          {{ t('charge.cost.chooseCurrency') }}
        </v-btn>
      </template>
      <template v-else>
        <div class="amount">
          <TileFigure
            :text="cost ? cost.amount.text : f.unknown()"
            :spoken="cost?.amount.spoken"
            :unknown="!cost"
            size="large"
          />
          {{ ' ' }}
          <SpokenText
            v-if="cost?.per100"
            class="per-100km d-block text-body-medium text-medium-emphasis mt-1"
            :text="cost.per100.text"
            :spoken="cost.per100.spoken"
          />
        </div>
        <template v-if="cost">
          <dl class="facts mt-4">
            <FactItem
              class="price"
              :label="t('trip.cost.price')"
              :value="cost.price.text"
              :spoken="cost.price.spoken"
            />
            <FactItem
              class="basis"
              :label="t('trip.cost.basis')"
              :value="t('trip.cost.charges', trip.cost?.price.charges ?? 0)"
            />
          </dl>
          <!-- The bar draws the legend's parts: the list says them. -->
          <div class="bar mt-4" aria-hidden="true">
            <span
              v-for="s in shares"
              :key="s.key"
              :style="{ width: `${s.pct}%`, background: s.color }"
            />
          </div>
          <ul class="legend mt-2" :aria-label="t('trip.cost.split')">
            <li v-for="s in shares" :key="s.key" class="text-body-small text-medium-emphasis">
              <span class="swatch" :style="{ background: s.color }" aria-hidden="true" />
              <SpokenText :text="s.text" :spoken="s.spoken" />
            </li>
          </ul>
        </template>
        <p v-for="n in notes" :key="n" class="note text-body-small text-medium-emphasis mt-3">
          {{ n }}
        </p>
      </template>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.facts {
  display: grid;
  gap: 1rem;
}
.bar {
  display: flex;
  height: 8px;
  overflow: hidden;
  border-radius: 4px;
  background: rgb(var(--v-theme-surface-variant));
}
.bar span {
  display: block;
  height: 100%;
}
.legend {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 14px;
  padding: 0;
  list-style: none;
}
.swatch {
  display: inline-block;
  width: 9px;
  height: 9px;
  margin-right: 5px;
  border-radius: 2px;
}
.amount {
  font-variant-numeric: tabular-nums;
}
</style>
