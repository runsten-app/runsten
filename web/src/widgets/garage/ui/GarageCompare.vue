<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  CompareToggle,
  chargeSplit,
  perHundred,
  shares,
  temperatureAxis,
  type CompareView,
  type GaragePeriod,
  type GarageVehicle,
} from '@/features/compare-vehicles'
import { FeatureUnavailable } from '@/features/view-limits'
import { majorUnits, useFormat } from '@/shared/lib'
import { ChartFrame, type Series } from '@/shared/ui/chart'
import { useTheme } from 'vuetify'

// GarageCompare sets the vehicles compared side by side over the same period, one color
// each, as their tiles have it: their consumption by outside temperature (the season
// weighs alike on all of them), what 100 km cost them month by month, where their energy
// was charged, and their battery (the loss while parked, the capacity's change since the
// first estimates). A vehicle without data for a chart is named under it, never drawn as
// a zero; with none to draw, that note is what the chart says, why included. monthly: false, where the account's limits refuse the statistics by interval: the
// cost by month says it is unavailable.
const props = defineProps<{
  garage: GarageVehicle[]
  labels: Map<string, string>
  colors: Map<string, string>
  period: GaragePeriod
  view: CompareView
  monthly: boolean
}>()
const { t } = useI18n()
const f = useFormat()
const theme = useTheme()

function token(name: string): string {
  const c = theme.current.value.colors[name]
  return typeof c === 'string' ? c : '#000'
}
const label = (id: string) => props.labels.get(id) ?? id
const color = (id: string) => props.colors.get(id) ?? token('chart-1')
const scope = computed(() =>
  t(
    'garage.compare.scope',
    {
      n: props.garage.length,
      period: props.period === '30d' ? t('garage.period.last30d') : t('garage.period.last12m'),
    },
    props.garage.length,
  ),
)
// names lists vehicles, as the language joins words.
const names = (ids: string[]) => f.list(ids.map(label))

// consumption: kWh/100 km by band of 5 °C, a line per vehicle on one axis of bands.
const consumption = computed(() => {
  // A band may have no estimate (a plug-in hybrid, trips without a known energy).
  const told = props.garage.filter((v) =>
    v.period?.trips_by_temperature.bands.some((b) => b.consumption_kwh_per_100km !== null),
  )
  const axis = temperatureAxis(told.map((v) => v.period?.trips_by_temperature.bands ?? []))
  const labels = axis.map((c) =>
    t('stats.band.between', { min: f.number(c), max: f.quantity(c + 5, 'celsius') }),
  )
  const series: Series[] = told.map((v) => {
    const bands = v.period?.trips_by_temperature.bands ?? []
    return {
      label: label(v.id),
      data: axis.map((c) => bands.find((b) => b.min_c === c)?.consumption_kwh_per_100km ?? null),
      color: color(v.id),
    }
  })
  const missing = props.garage.filter((v) => v.period && !told.includes(v))
  return {
    labels,
    series,
    summary: told.length
      ? t('garage.compare.consumption.summary', { vehicles: names(told.map((v) => v.id)) })
      : t('garage.compare.consumption.none'),
    note: missing.length
      ? t(
          'garage.compare.consumption.without',
          { vehicles: names(missing.map((v) => v.id)) },
          missing.length,
        )
      : null,
  }
})

// The months of the last 12, as every vehicle's monthly statistics split them.
const months = computed(() => props.garage.find((v) => v.months)?.months?.buckets ?? [])
const monthLabels = computed(() => months.value.map((b) => f.bucket(b.start, 'month')))
const currency = computed(
  () => props.garage.find((v) => v.months?.currency)?.months?.currency ?? null,
)
// Every vehicle's months read: before, no currency is no reason to say it is missing.
const monthsRead = computed(() => props.garage.every((v) => v.months))

// cost: what 100 km cost each vehicle, month by month, its lowest bound: a range would
// make the bars of several vehicles unreadable, and the bounds are one most of the time.
const cost = computed(() => {
  const c = currency.value
  if (!c) return null
  const series: Series[] = props.garage.map((v) => ({
    label: label(v.id),
    data: months.value.map((_, i) => {
      const b = v.months?.buckets[i]
      const r = b && perHundred(b.trips)
      return r ? majorUnits(r.min_minor, c.minor_digits) : null
    }),
    color: color(v.id),
  }))
  const any = series.some((s) => s.data.some((d) => d !== null))
  return {
    series,
    summary: any ? t('garage.compare.cost.summary') : t('garage.compare.cost.none'),
    format: (v: number | null) =>
      v === null
        ? f.unknown()
        : t('stats.chart.costPerDistance.value', { amount: f.majorAmount(v, c) }),
    unit: t('stats.chart.costPerDistance.unit', { currency: f.currencySymbol(c.code) }),
  }
})

// charging: the share of the energy charged at the account's places, on AC and on DC
// outside them, and unknown, a stacked bar per vehicle.
const charging = computed(() => {
  const split = props.garage.map((v) => ({
    id: v.id,
    shares: v.period ? shares(chargeSplit(v.period)) : null,
  }))
  const told = split.filter((s) => s.shares)
  // Read, but without a charge over the period.
  const idle = props.garage.filter((v) => v.period?.totals.charges.count === 0).map((v) => v.id)
  // Charges whose energy is unknown: left out of the shares, or the whole bar.
  const unknown = props.garage
    .filter((v) => (v.period?.totals.charges.energy_soc_unknown ?? 0) > 0)
    .map((v) => v.id)
  const part = (k: 'places' | 'ac' | 'dc' | 'unknown') => told.map((s) => s.shares?.[k] ?? null)
  const series: Series[] = [
    { label: t('garage.compare.charging.places'), data: part('places'), color: token('chart-1') },
    { label: t('garage.compare.charging.ac'), data: part('ac'), color: token('chart-2') },
    { label: t('garage.compare.charging.dc'), data: part('dc'), color: token('chart-5') },
  ]
  if (told.some((s) => (s.shares?.unknown ?? 0) > 0))
    series.push({
      label: t('garage.compare.charging.unknown'),
      data: part('unknown'),
      color: token('chart-3'),
    })
  return {
    labels: told.map((s) => label(s.id)),
    series,
    summary: told.length ? t('garage.compare.charging.summary') : t('garage.compare.charging.none'),
    note:
      [
        ...(idle.length
          ? [t('garage.compare.charging.without', { vehicles: names(idle) }, idle.length)]
          : []),
        ...(unknown.length
          ? [
              t(
                'garage.compare.charging.energyUnknown',
                { vehicles: names(unknown) },
                unknown.length,
              ),
            ]
          : []),
      ].join(' ') || null,
  }
})

// perVehicle is one series per vehicle, its value at its own place only: stacked, each
// bar takes its vehicle's color.
function perVehicle(ids: string[], value: (id: string) => number | null): Series[] {
  return ids.map((id, i) => ({
    label: label(id),
    data: ids.map((_, k) => (k === i ? value(id) : null)),
    color: color(id),
  }))
}

// parked: the state of charge lost per day while parked, over the period.
const parked = computed(() => {
  const value = (id: string) =>
    props.garage.find((v) => v.id === id)?.period?.totals.parked.soc_loss_pct_per_day ?? null
  const told = props.garage.map((v) => v.id).filter((id) => value(id) !== null)
  return {
    labels: told.map(label),
    series: perVehicle(told, value),
    summary: told.length
      ? t('garage.compare.battery.parkedSummary')
      : t('garage.compare.battery.parkedNone'),
  }
})
const perDay = (v: number | null) =>
  v === null ? f.unknown() : t('stats.tile.perDay', { value: f.quantity(v, 'percent', 1) })

// capacity: the change of the estimated capacity since the vehicle's first estimates,
// over its whole history: the period does not bound it.
const capacity = computed(() => {
  const change = (id: string) => props.garage.find((v) => v.id === id)?.battery?.change ?? null
  const told = props.garage.map((v) => v.id).filter((id) => change(id))
  const since = told.map((id) => {
    const c = change(id)
    return c
      ? t('garage.compare.battery.since', {
          vehicle: label(id),
          since: f.calendarMonth(f.dayKey(c.since)),
        })
      : ''
  })
  return {
    labels: told.map(label),
    series: perVehicle(told, (id) => change(id)?.change_pct ?? null),
    summary: told.length
      ? t('garage.compare.battery.capacitySummary')
      : t('garage.compare.battery.capacityNone'),
    hint: since.length ? f.list(since) : undefined,
  }
})
</script>

<template>
  <section class="garage-compare">
    <div class="d-flex flex-wrap align-center ga-3 mb-1">
      <h2 class="text-title-large me-auto">{{ t('garage.compare.title') }}</h2>
      <CompareToggle />
    </div>
    <p class="scope text-body-medium text-medium-emphasis mb-3">{{ scope }}</p>
    <div class="charts">
      <ChartFrame
        v-if="view === 'consumption'"
        class="by-temperature wide"
        kind="line"
        :title="t('garage.compare.consumption.title')"
        :summary="consumption.summary"
        :labels="consumption.labels"
        :series="consumption.series"
        unit="kWh/100 km"
        :interval-header="t('stats.chart.byTemperature.header')"
        :format="(v) => f.quantity(v, 'kWhPer100km')"
        :hint="consumption.note ?? t('garage.compare.consumption.hint')"
        :empty="consumption.series.length ? undefined : (consumption.note ?? consumption.summary)"
        :selectable="false"
      />
      <template v-else-if="view === 'cost'">
        <FeatureUnavailable v-if="!monthly" feature="stats" class="wide" />
        <v-progress-linear
          v-else-if="!monthsRead"
          indeterminate
          class="wide"
          :aria-label="t('stats.loading')"
        />
        <ChartFrame
          v-else-if="cost"
          class="cost-by-month wide"
          kind="bar"
          :title="t('garage.compare.cost.title')"
          :summary="cost.summary"
          :labels="monthLabels"
          :series="cost.series"
          :unit="cost.unit"
          :interval-header="t('stats.bucket.monthHeader')"
          :format="cost.format"
          :hint="t('garage.compare.cost.hint')"
          :empty="
            cost.series.some((s) => s.data.some((d) => d !== null)) ? undefined : cost.summary
          "
          :selectable="false"
        />
        <v-alert v-else type="info" variant="tonal" class="wide">
          {{ t('stats.view.noCurrency') }}
        </v-alert>
      </template>
      <ChartFrame
        v-else-if="view === 'charging'"
        class="charge-split wide"
        kind="bar"
        stacked
        :title="t('garage.compare.charging.title')"
        :summary="charging.summary"
        :labels="charging.labels"
        :series="charging.series"
        unit="%"
        :interval-header="t('garage.compare.vehicle')"
        :y-max="100"
        :format="(v) => f.quantity(v, 'percent', 0)"
        :hint="charging.note ?? t('garage.compare.charging.hint')"
        :empty="charging.labels.length ? undefined : (charging.note ?? charging.summary)"
        :selectable="false"
      />
      <template v-else>
        <ChartFrame
          class="parked"
          kind="bar"
          stacked
          :title="t('garage.compare.battery.parkedTitle')"
          :summary="parked.summary"
          :labels="parked.labels"
          :series="parked.series"
          :unit="t('stats.chart.parked.unit')"
          :interval-header="t('garage.compare.vehicle')"
          :format="perDay"
          :empty="parked.labels.length ? undefined : parked.summary"
          :selectable="false"
        />
        <ChartFrame
          class="capacity"
          kind="bar"
          stacked
          :title="t('garage.compare.battery.capacityTitle')"
          :summary="capacity.summary"
          :labels="capacity.labels"
          :series="capacity.series"
          unit="%"
          :interval-header="t('garage.compare.vehicle')"
          :format="f.signedPercent"
          :hint="capacity.hint ?? t('garage.compare.battery.capacityHint')"
          :empty="capacity.labels.length ? undefined : capacity.summary"
          :selectable="false"
        />
      </template>
    </div>
  </section>
</template>

<style scoped>
.charts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.wide {
  grid-column: 1 / -1;
}
@media (max-width: 959.98px) {
  .charts {
    grid-template-columns: minmax(0, 1fr);
    gap: 8px;
  }
}
</style>
