<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Battery } from '@/entities/battery'
import { useBattery } from '@/features/view-battery'
import { isApiError } from '@/shared/api'
import { useFormat } from '@/shared/lib'
import { TileFigure } from '@/shared/ui'
import { remainingOf } from '../model/remaining'
import CapacityGauge from './CapacityGauge.vue'

// BatteryTiles are the figures of the estimated capacity: each with what it rests on
// (the reference, the first estimate) and what it is not (a measurement, and the
// thresholds of the aggregates). The first is a gauge of the remaining capacity, the
// estimate over the reference; the deviation from the reference is that same figure, so
// it has no tile of its own. The counts of the estimates kept and set aside open the
// reasons of the filters in a dialog.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const f = useFormat()
const { battery, isPending, error } = useBattery(() => props.vehicle)

type Tile = { key: string; label: string; value: string; notes: string[] }

// minEstimates is the API's MinEstimates: the fewest estimates it shows a current
// capacity from. A charge can give two, so the count left is an upper bound in charges.
const minEstimates = 5

const remaining = computed(() => (battery.value ? remainingOf(battery.value) : null))

const tiles = computed<Tile[]>(() => {
  const b = battery.value
  if (!b) return []
  return [
    {
      key: 'capacity',
      // Without a reference there is nothing to compare with: the estimate alone, in kWh.
      label: remaining.value ? t('battery.tile.remaining') : t('battery.tile.capacity'),
      // Under 5 estimates the median would say nothing: the series still shows them.
      value: remaining.value
        ? t('battery.tile.remainingValue', { pct: f.quantity(remaining.value.pct, 'percent') })
        : b.current
          ? f.quantity(b.current.capacity_kwh, 'kWh')
          : t('battery.tile.notEnough'),
      notes: b.current
        ? [
            ...(remaining.value
              ? [t('battery.tile.estimated', { kwh: f.quantity(b.current.capacity_kwh, 'kWh') })]
              : []),
            t('battery.tile.between', {
              q1: f.quantity(b.current.q1_kwh, 'kWh'),
              q3: f.quantity(b.current.q3_kwh, 'kWh'),
            }),
          ]
        : [t('battery.tile.need', Math.max(1, minEstimates - b.estimates.length))],
    },
    {
      key: 'reference',
      label: t('battery.tile.reference'),
      value: b.reference ? f.quantity(b.reference.capacity_kwh, 'kWh') : f.unknown(),
      notes: b.reference
        ? [
            b.reference.source === 'catalog_net'
              ? t('battery.tile.referenceCatalog')
              : t('battery.tile.referenceApi'),
          ]
        : [],
    },
    {
      key: 'change',
      label: t('battery.tile.change'),
      // The evolution waits for 12 months of data and two windows of 20 estimates.
      value: b.change ? f.signedPercent(b.change.change_pct) : t('battery.tile.inTwelveMonths'),
      notes: b.change
        ? [t('battery.tile.since', { date: f.calendarDay(b.change.since.slice(0, 10)) })]
        : [],
    },
    {
      key: 'cycles',
      label: t('battery.tile.cycles'),
      value: b.cycles === null ? f.unknown() : f.number(b.cycles, 1),
      notes: [t('battery.tile.cyclesNote')],
    },
    {
      key: 'kept',
      label: t('battery.tile.kept'),
      // A charge can give two estimates (its power, its receipt): the driver counts
      // charges, and the tile counts the charges, not the estimates.
      value: f.number(new Set(b.estimates.map((e) => e.charge)).size),
      notes: [t('battery.tile.setAside', excludedTotal(b.excluded))],
    },
  ]
})

function excludedTotal(e: Battery['excluded']): number {
  return (
    e.reconstructed +
    e.no_power +
    e.high_soc +
    e.span_soc +
    e.power_gap +
    e.low_power +
    e.implausible
  )
}

// Each reason is written out, so that the i18n lint sees every key in use; the counts are
// the API's, in the order its filters apply them.
const reasons = computed(() => {
  const e = battery.value?.excluded
  if (!e) return []
  return [
    { key: 'reconstructed', count: e.reconstructed, text: t('battery.why.reconstructed') },
    { key: 'noPower', count: e.no_power, text: t('battery.why.noPower') },
    { key: 'highSoc', count: e.high_soc, text: t('battery.why.highSoc') },
    { key: 'spanSoc', count: e.span_soc, text: t('battery.why.spanSoc') },
    { key: 'powerGap', count: e.power_gap, text: t('battery.why.powerGap') },
    { key: 'lowPower', count: e.low_power, text: t('battery.why.lowPower') },
    { key: 'implausible', count: e.implausible, text: t('battery.why.implausible') },
  ]
})
const why = ref(false)

const errorMessage = computed(() => {
  if (isApiError(error.value, 'not_found')) return t('vehicle.notFound')
  if (isApiError(error.value, 'invalid_parameter')) return t('battery.tooMany')
  return t('battery.error')
})
</script>

<template>
  <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ errorMessage }}</v-alert>
  <v-progress-linear v-else-if="isPending" indeterminate :aria-label="t('battery.loading')" />
  <!-- A <dl> holds div groups of dt and dd only: the dialog opens from the dd. -->
  <dl v-else-if="tiles.length" class="battery-tiles mb-3">
    <div v-for="tile in tiles" :key="tile.key" :class="['tile', tile.key]">
      <dt class="label">{{ tile.label }}</dt>
      <dd>
        <!-- The first tile draws its figure in the gauge; without a reference, there is
             nothing to draw it against. -->
        <div v-if="tile.key === 'capacity' && battery?.reference" class="gauge-row">
          <CapacityGauge :remaining :label="tile.label">
            <TileFigure
              class="value"
              :text="tile.value"
              :size="remaining ? 'kpi' : 'small'"
              :unknown="!remaining"
            />
          </CapacityGauge>
          <div class="notes">
            <span v-for="n in tile.notes" :key="n" class="note d-block">{{ n }}</span>
          </div>
        </div>
        <template v-else>
          <TileFigure
            class="value"
            :text="tile.value"
            size="kpi"
            :unknown="tile.value === f.unknown()"
          />
          <span v-for="n in tile.notes" :key="n" class="note d-block">{{ n }}</span>
        </template>
        <!-- The reasons are the API's filters: a dialog keeps the tile compact, and the
             focus inside while it is open. -->
        <v-dialog
          v-if="tile.key === 'kept'"
          v-model="why"
          max-width="34rem"
          aria-labelledby="battery-why-title"
        >
          <template #activator="{ props: activator }">
            <v-btn
              v-bind="activator"
              variant="text"
              size="small"
              color="primary"
              class="why mt-1 px-0"
            >
              {{ t('battery.tile.why') }}
            </v-btn>
          </template>
          <v-card>
            <v-card-title id="battery-why-title" tag="h2" class="text-title-large text-wrap">
              {{ t('battery.why.title') }}
            </v-card-title>
            <v-card-text>
              <p class="text-body-medium mb-3">{{ t('battery.why.intro') }}</p>
              <ul class="reasons">
                <li v-for="r in reasons" :key="r.key" class="text-body-medium mb-2">
                  <span class="font-weight-medium">{{ f.number(r.count) }}</span>
                  {{ r.text }}
                </li>
              </ul>
            </v-card-text>
            <v-card-actions>
              <v-spacer />
              <v-btn variant="text" @click="why = false">{{ t('battery.why.close') }}</v-btn>
            </v-card-actions>
          </v-card>
        </v-dialog>
      </dd>
    </div>
  </dl>
</template>

<style scoped>
/* The capacity takes the left half, over two rows; the four others fill the right. */
.battery-tiles {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}
.tile {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 20px;
  border-radius: var(--v-tile-radius);
  background: rgb(var(--v-theme-surface));
  min-width: 0;
}
.tile.capacity {
  grid-column: span 2;
  grid-row: span 2;
}
@media (max-width: 959.98px) {
  .battery-tiles {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 599.98px) {
  .battery-tiles {
    gap: 8px;
  }
  .tile {
    padding: 16px;
    border-radius: var(--v-tile-radius-phone);
  }
  .tile.capacity {
    grid-row: auto;
  }
}
.label {
  font-size: 0.8125rem;
  color: rgb(var(--v-theme-text-secondary));
}
dd {
  margin: 0;
}
.note {
  margin-top: 4px;
  font-size: 0.75rem;
  line-height: 1.4;
  color: rgb(var(--v-theme-text-secondary));
}
.gauge-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 24px;
}
.notes .note {
  font-size: 0.875rem;
}
.reasons {
  list-style: none;
  padding: 0;
  margin: 0;
}
</style>
