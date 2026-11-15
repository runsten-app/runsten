<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { usePurposeLabel, type PurposeFilter, type Trip } from '@/entities/trip'
import { vehicleLabel } from '@/entities/vehicle'
import { useAllTrips } from '@/features/browse-trips'
import { useVehicle } from '@/features/select-vehicle'
import { useFormat, type Spoken } from '@/shared/lib'
import { SpokenText } from '@/shared/ui'

// TripReport is a period's trips of one purpose as a page to print, or to save as PDF from
// the browser: for a mileage allowance or an expense report. Oldest first, with the
// odometer at each end, what each cost, and the totals.
const props = defineProps<{
  vehicle: string
  from?: string
  to?: string
  purpose?: PurposeFilter
}>()
const { t } = useI18n()
const f = useFormat()
const purposeLabel = usePurposeLabel()
const { trips, isPending, error } = useAllTrips(
  () => props.vehicle,
  () => props.from,
  () => props.to,
  () => props.purpose,
)
const { vehicle } = useVehicle(() => props.vehicle)

const rows = computed(() => [...trips.value].reverse())
const title = computed(() =>
  props.purpose
    ? t('report.titleOf', {
        purpose: purposeLabel(props.purpose),
      })
    : t('report.title'),
)
const period = computed(() => {
  if (!props.from && !props.to) return t('report.allTime')
  const day = (iso: string | undefined) => (iso ? f.day(iso) : '…')
  // to is excluded: the period ends on the day before.
  const last = props.to && new Date(new Date(props.to).getTime() - 1).toISOString()
  return `${day(props.from)} – ${day(last)}`
})

const place = (name: string | null | undefined, address: string | null) =>
  name ?? address ?? t('trip.elsewhere')
const odometer = (tr: Trip) =>
  tr.start_odometer_km === null && tr.end_odometer_km === null
    ? f.unknown()
    : f.change(tr.start_odometer_km, tr.end_odometer_km, 'km')
const lines = computed(() =>
  rows.value.map((tr) => ({
    tr,
    cost: (tr.cost ? f.estimate(tr.cost, tr.cost.currency) : null) as Spoken | null,
  })),
)

const totals = computed(() => {
  const km = rows.value.reduce((s, tr) => s + (tr.distance_km ?? 0), 0)
  const priced = rows.value.filter((tr) => tr.cost)
  const currency = priced[0]?.cost?.currency
  const amount = currency
    ? f.estimate(
        {
          min_minor: priced.reduce((s, tr) => s + (tr.cost?.min_minor ?? 0), 0),
          max_minor: priced.reduce((s, tr) => s + (tr.cost?.max_minor ?? 0), 0),
        },
        currency,
      )
    : null
  return { km, amount, unpriced: rows.value.length - priced.length }
})
</script>

<template>
  <v-progress-linear v-if="isPending" indeterminate :aria-label="t('trips.loading')" />
  <v-alert v-else-if="error" type="error" variant="tonal">{{ t('trips.error') }}</v-alert>
  <article v-else class="trip-report">
    <header class="d-flex flex-wrap justify-space-between ga-2 mb-4">
      <div>
        <h1 class="text-headline-small">{{ title }}</h1>
        <p class="text-body-medium">{{ period }}</p>
        <p v-if="vehicle" class="vehicle text-body-medium text-medium-emphasis">
          {{ vehicleLabel(vehicle) }} · {{ vehicle.vin }}
        </p>
      </div>
    </header>
    <p v-if="!rows.length" class="empty text-body-large text-medium-emphasis">
      {{ t('trips.emptyPeriod') }}
    </p>
    <div v-else class="table-wrap">
      <v-table density="compact" class="report-table">
        <thead>
          <tr>
            <th scope="col">{{ t('report.date') }}</th>
            <th scope="col">{{ t('report.from') }}</th>
            <th scope="col">{{ t('report.to') }}</th>
            <th scope="col" class="text-end">{{ t('trip.odometer') }}</th>
            <th scope="col" class="text-end">{{ t('trip.distance') }}</th>
            <th scope="col" class="text-end">{{ t('trip.cost.title') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="{ tr, cost } in lines" :key="tr.id">
            <td>{{ f.day(tr.start.after) }}</td>
            <td>{{ place(tr.start_place?.name, tr.start_address) }}</td>
            <td>{{ place(tr.end_place?.name, tr.end_address) }}</td>
            <td class="text-end">{{ odometer(tr) }}</td>
            <td class="text-end">{{ f.quantity(tr.distance_km, 'km') }}</td>
            <td class="text-end">
              <SpokenText v-if="cost" :text="cost.text" :spoken="cost.spoken" />
              <template v-else>{{ f.unknown() }}</template>
            </td>
          </tr>
        </tbody>
        <tfoot>
          <tr class="totals">
            <th scope="row" colspan="4">{{ t('report.trips', rows.length) }}</th>
            <td class="text-end">{{ f.quantity(totals.km, 'km') }}</td>
            <td class="text-end">
              <SpokenText
                v-if="totals.amount"
                :text="totals.amount.text"
                :spoken="totals.amount.spoken"
              />
            </td>
          </tr>
        </tfoot>
      </v-table>
    </div>
    <p class="note text-body-small text-medium-emphasis mt-3">{{ t('report.note') }}</p>
    <p v-if="totals.unpriced" class="note text-body-small text-medium-emphasis">
      {{ t('report.unpriced', totals.unpriced) }}
    </p>
  </article>
</template>

<style scoped>
.table-wrap {
  overflow-x: auto;
}
.report-table td,
.report-table th {
  white-space: nowrap;
}
.totals th,
.totals td {
  font-weight: 600;
}
</style>
