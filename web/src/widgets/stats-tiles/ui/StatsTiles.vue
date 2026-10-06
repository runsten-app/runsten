<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useStats } from '@/features/view-stats'
import { isApiError } from '@/shared/api'
import { useFormat, type Bucket } from '@/shared/lib'
import { TileFigure } from '@/shared/ui'

// StatsTiles are the totals of the period: each figure with what it lacks (unknown
// values, reconstructed events) and whether it is an estimate. The cost of the charges
// needs the account's currency; the entered costs no charge takes are told apart, never
// added to it (they would count a charge twice).
const props = defineProps<{ vehicle: string; from?: string; to?: string; bucket: Bucket }>()
const { t } = useI18n()
const f = useFormat()
const { stats, isPending, error } = useStats(
  () => props.vehicle,
  () => props.from,
  () => props.to,
  () => props.bucket,
)

// A range is shown as its two bounds, at least and at most: never a midpoint.
type Tile = {
  key: string
  label: string
  value: string
  spoken?: string
  range?: { min: string; max: string }
  notes: string[]
}

const tiles = computed<Tile[]>(() => {
  const s = stats.value?.totals
  if (!s) return []
  const { trips: tr, charges: ch, parked: pk } = s
  const note = (cond: unknown, text: string) => (cond ? [text] : [])
  return [
    {
      key: 'distance',
      label: t('stats.tile.distance'),
      value: f.quantity(tr.distance_km, 'km'),
      notes: note(tr.distance_unknown, t('stats.tile.distanceUnknown', tr.distance_unknown)),
    },
    {
      key: 'trips',
      label: t('stats.tile.trips'),
      value: String(tr.count),
      notes: note(tr.reconstructed, t('stats.trips.reconstructed', tr.reconstructed)),
    },
    {
      key: 'driving-time',
      label: t('stats.tile.drivingTime'),
      value: f.totalDuration(tr.driving_time_s),
      range: f.totalDurationBounds(tr.driving_time_s) ?? undefined,
      notes: note(
        tr.driving_time_unknown,
        t('stats.tile.withoutDuration', tr.driving_time_unknown),
      ),
    },
    {
      key: 'consumption',
      label: t('stats.tile.consumption'),
      value: f.quantity(tr.consumption_kwh_per_100km, 'kWhPer100km'),
      notes: [t('stats.tile.consumptionNote')],
    },
    {
      key: 'energy',
      label: t('stats.tile.energy'),
      value: f.quantity(ch.energy_soc_kwh, 'kWh', 0),
      notes: [
        t('stats.tile.energySoc'),
        ...note(ch.energy_soc_unknown, t('stats.charges.energyUnknown', ch.energy_soc_unknown)),
        t('stats.tile.energyPower', { value: f.quantity(ch.energy_power_kwh, 'kWh', 0) }),
        ...note(
          ch.energy_power_unknown,
          t('stats.tile.energyPowerUnknown', ch.energy_power_unknown),
        ),
      ],
    },
    {
      key: 'charges',
      label: t('stats.tile.charges'),
      value: String(ch.count),
      notes: [
        t('stats.charges.types', { ac: ch.by_type.ac.count, dc: ch.by_type.dc.count }),
        ...note(ch.by_type.unknown.count, t('stats.charges.typeUnknown', ch.by_type.unknown.count)),
      ],
    },
    ...costTile(ch),
    {
      key: 'parked',
      label: t('stats.tile.parked'),
      value:
        pk.soc_loss_pct_per_day === null
          ? f.unknown()
          : t('stats.tile.perDay', { value: f.quantity(pk.soc_loss_pct_per_day, 'percent', 1) }),
      notes: [
        t('stats.tile.parkedNote'),
        ...note(pk.soc_loss_unknown, t('stats.tile.parkedUnknown', pk.soc_loss_unknown)),
      ],
    },
  ]
})

function costTile(ch: NonNullable<typeof stats.value>['totals']['charges']): Tile[] {
  const currency = stats.value?.currency
  if (!currency) return []
  const note = (n: number, text: string) => (n ? [text] : [])
  // All unknown: no sum of nothing, which would read as free.
  const cost =
    ch.count && ch.cost_unknown === ch.count
      ? { text: f.unknown(), spoken: f.unknown() }
      : f.amounts(ch.cost, currency)
  return [
    {
      key: 'cost',
      label: t('stats.tile.cost'),
      value: cost.text,
      spoken: cost.spoken,
      notes: [
        t('stats.tile.costNote'),
        ...note(ch.cost_unknown, t('stats.tile.costUnknown', ch.cost_unknown)),
        ...note(ch.cost_entered, t('stats.charges.costEntered', ch.cost_entered)),
      ],
    },
  ]
}

// orphans are the entered costs without a charge whose window starts in the period.
const orphans = computed(() => {
  const s = stats.value
  const o = s?.totals.charges.orphaned_costs
  if (!s?.currency || !o?.count) return ''
  return t('stats.orphans', { n: o.count, amount: f.amount(o.amount_minor, s.currency) }, o.count)
})

const errorMessage = computed(() => {
  if (isApiError(error.value, 'not_found')) return t('vehicle.notFound')
  if (isApiError(error.value, 'invalid_parameter')) return t('stats.tooMany')
  return t('stats.error')
})
</script>

<template>
  <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ errorMessage }}</v-alert>
  <v-progress-linear v-else-if="isPending" indeterminate :aria-label="t('stats.loading')" />
  <!-- A <dl> holds div groups of dt and dd only: the notes are inside the dd. -->
  <dl v-else-if="tiles.length" class="stats-tiles mb-3">
    <div v-for="tile in tiles" :key="tile.key" :class="['tile', tile.key]">
      <dt class="label">{{ tile.label }}</dt>
      <dd>
        <span v-if="tile.range" class="value range">
          <span class="bound">
            <span class="caption">{{ t('common.atLeast') }}</span
            >{{ ' ' }}
            <TileFigure :text="tile.range.min" size="kpi" />
          </span>
          {{ ' ' }}
          <span class="bound">
            <span class="caption">{{ t('common.atMost') }}</span
            >{{ ' ' }}
            <TileFigure :text="tile.range.max" size="kpi" />
          </span>
        </span>
        <TileFigure
          v-else
          class="value"
          :text="tile.value"
          :spoken="tile.spoken"
          :unknown="tile.value === f.unknown()"
          size="kpi"
        />
        <span v-for="n in tile.notes" :key="n" class="note d-block">{{ n }}</span>
      </dd>
    </div>
  </dl>
  <p v-if="tiles.length && orphans" class="orphans text-body-medium mb-4">
    {{ orphans }}
    <router-link :to="{ name: 'settings' }" class="text-primary">{{
      t('stats.orphansLink')
    }}</router-link>
  </p>
</template>

<style scoped>
.stats-tiles {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}
.tile {
  padding: 20px;
  border-radius: var(--v-tile-radius);
  background: rgb(var(--v-theme-surface));
  min-width: 0;
}
@media (max-width: 959.98px) {
  .stats-tiles {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 599.98px) {
  .stats-tiles {
    gap: 8px;
  }
  .tile {
    padding: 16px;
    border-radius: var(--v-tile-radius-phone);
  }
}
.label {
  margin-bottom: 4px;
  font-size: 0.8125rem;
  color: rgb(var(--v-theme-text-secondary));
}
dd {
  margin: 0;
}
.range {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 20px;
}
.bound {
  display: flex;
  flex-direction: column;
}
.caption {
  font-size: 0.6875rem;
  color: rgb(var(--v-theme-text-secondary));
}
.note {
  margin-top: 6px;
  font-size: 0.75rem;
  line-height: 1.4;
  color: rgb(var(--v-theme-text-secondary));
}
</style>
