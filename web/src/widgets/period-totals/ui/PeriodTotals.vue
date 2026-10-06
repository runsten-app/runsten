<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useStats } from '@/features/view-stats'
import { useFormat, type Spoken } from '@/shared/lib'
import { SpokenText } from '@/shared/ui'

// PeriodTotals sums the trips or the charges of the list's period, in one line. An event
// counts where it started: one across a limit of the period is listed, not counted, and
// the line says so ("started in the period"). The cost of the charges comes with the
// account's currency only: without it, none is known.
const props = defineProps<{
  vehicle: string
  from?: string
  to?: string
  kind: 'trips' | 'charges'
}>()
const { t } = useI18n()
const f = useFormat()
const { stats, error } = useStats(
  () => props.vehicle,
  () => props.from,
  () => props.to,
)
const filtered = computed(() => !!(props.from || props.to))

type Totals = NonNullable<typeof stats.value>['totals']
const said = (text: string): Spoken => ({ text, spoken: text })

function tripParts(s: Totals['trips']): string[] {
  const parts = [
    filtered.value ? t('stats.trips.inPeriod', s.count) : t('stats.trips.all', s.count),
  ]
  if (s.reconstructed) parts.push(t('stats.trips.reconstructed', s.reconstructed))
  parts.push(f.quantity(s.distance_km, 'km'))
  if (s.distance_unknown) parts.push(t('stats.trips.distanceUnknown', s.distance_unknown))
  const c = s.consumption_kwh_per_100km
  parts.push(
    c === null
      ? t('stats.trips.consumptionUnknown')
      : t('stats.trips.consumption', { value: f.quantity(c, 'kWhPer100km') }),
  )
  return parts
}

function chargeParts(s: Totals['charges']): string[] {
  const parts = [
    filtered.value ? t('stats.charges.inPeriod', s.count) : t('stats.charges.all', s.count),
  ]
  if (s.reconstructed) parts.push(t('stats.charges.reconstructed', s.reconstructed))
  parts.push(t('stats.charges.energy', { value: f.quantity(s.energy_soc_kwh, 'kWh', 0) }))
  if (s.energy_soc_unknown) parts.push(t('stats.charges.energyUnknown', s.energy_soc_unknown))
  const { ac, dc, unknown } = s.by_type
  if (ac.count || dc.count) parts.push(t('stats.charges.types', { ac: ac.count, dc: dc.count }))
  if (unknown.count) parts.push(t('stats.charges.typeUnknown', unknown.count))
  return parts
}

// costParts sum the known costs ("€42.10 – €45.60"), then tell those unknown and entered.
function costParts(s: Totals['charges']): Spoken[] {
  const currency = stats.value?.currency
  if (!currency) return []
  const parts: Spoken[] = []
  if (s.cost_unknown < s.count) parts.push(f.amounts(s.cost, currency))
  if (s.cost_unknown) parts.push(said(t('stats.charges.costUnknown', s.cost_unknown)))
  if (s.cost_entered) parts.push(said(t('stats.charges.costEntered', s.cost_entered)))
  return parts
}

// Nothing without an event: the list says it is empty.
const parts = computed<Spoken[]>(() => {
  const s = stats.value?.totals
  if (!s) return []
  if (props.kind === 'trips') return s.trips.count ? tripParts(s.trips).map(said) : []
  if (!s.charges.count) return []
  return [...chargeParts(s.charges).map(said), ...costParts(s.charges)]
})
</script>

<template>
  <p v-if="parts.length" class="period-totals text-body-medium mb-3">
    <template v-for="(p, i) in parts" :key="i"
      ><template v-if="i"> · </template><SpokenText :text="p.text" :spoken="p.spoken"
    /></template>
  </p>
  <p v-else-if="error" class="period-totals text-body-medium text-medium-emphasis mb-3">
    {{ t('stats.error') }}
  </p>
</template>
