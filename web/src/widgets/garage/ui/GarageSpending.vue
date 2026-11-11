<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { accountTotals, costKnown, type GarageVehicle } from '@/features/compare-vehicles'
import { majorUnits, useFormat } from '@/shared/lib'
import { ChartFrame, type Series } from '@/shared/ui/chart'

// GarageSpending is what the electricity of every vehicle of the account cost, month by
// month over the last 12, a bar a month stacked by vehicle: hidden ones included, the
// spending is the account's. Each vehicle's part is its lowest bound, as the cost per 100 km
// of the comparison; a month whose costs are all unknown has no part, never a zero.
const props = defineProps<{
  garage: GarageVehicle[]
  labels: Map<string, string>
  colors: Map<string, string>
}>()
const { t } = useI18n()
const f = useFormat()

const months = computed(() => props.garage.find((v) => v.months)?.months?.buckets ?? [])
const monthLabels = computed(() => months.value.map((b) => f.bucket(b.start, 'month')))
const currency = computed(
  () => props.garage.find((v) => v.months?.currency)?.months?.currency ?? null,
)

const spending = computed(() => {
  const c = currency.value
  if (!c) return null
  const series: Series[] = props.garage.map((v) => ({
    label: props.labels.get(v.id) ?? v.id,
    data: months.value.map((_, i) => {
      const b = v.months?.buckets[i]
      return b && b.charges.count && costKnown(b.charges)
        ? majorUnits(b.charges.cost.min_minor, c.minor_digits)
        : null
    }),
    color: props.colors.get(v.id) ?? '',
  }))
  const totals = accountTotals(props.garage.flatMap((v) => (v.months ? [v.months.totals] : [])))
  const summary = totals.costKnown
    ? t('garage.spending.summary', { total: f.amounts(totals.cost, c).spoken })
    : t('garage.spending.none')
  // Costs unknown beside known ones: the bars and the sum leave them out, and say so.
  const partial = totals.costKnown && totals.costUnknown
  return {
    series,
    summary: partial ? `${summary} ${t('garage.spending.partial', totals.costUnknown)}` : summary,
    partial: partial ? t('garage.spending.partial', totals.costUnknown) : null,
    empty: totals.costKnown ? undefined : summary,
    format: (v: number | null) => (v === null ? f.unknown() : f.majorAmount(v, c)),
    unit: f.currencySymbol(c.code),
  }
})
</script>

<template>
  <ChartFrame
    v-if="spending"
    class="garage-spending"
    kind="bar"
    stacked
    :title="t('garage.spending.title')"
    :summary="spending.summary"
    :labels="monthLabels"
    :series="spending.series"
    :unit="spending.unit"
    :interval-header="t('stats.bucket.monthHeader')"
    :format="spending.format"
    :hint="[t('garage.spending.hint'), spending.partial].filter(Boolean).join(' ')"
    :empty="spending.empty"
    :selectable="false"
  />
</template>
