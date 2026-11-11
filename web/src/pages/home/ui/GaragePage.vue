<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { vehicleLabels, type Vehicle } from '@/entities/vehicle'
import { GaragePeriodToggle, useGarage, useGarageView } from '@/features/compare-vehicles'
import { FeatureUnavailable, useLimits } from '@/features/view-limits'
import {
  GarageCompare,
  GarageSpending,
  GarageTiles,
  GarageTotals,
  useVehicleColors,
} from '@/widgets/garage'

// GaragePage is where an account with several vehicles lands: its totals over the period
// (the last 30 days or 12 months), its vehicles side by side, a tile each leading to the
// vehicle's page, then their comparison and the spending per month. A vehicle can be
// hidden from the comparison, for this visit only. Loaded on demand: Chart.js comes with
// it. Where the account's limits leave the statistics out, the tiles tell the state only.
const props = defineProps<{ vehicles: Vehicle[] }>()
const { t } = useI18n()
const { lacks } = useLimits()
const stats = computed(() => !lacks('stats'))
const { period, view } = useGarageView()

const ids = computed(() => props.vehicles.map((v) => v.id))
const labels = computed(() => vehicleLabels(props.vehicles))
const colors = useVehicleColors(ids)
const { garage, isPending, error } = useGarage(ids, period, {
  stats,
  battery: () => stats.value && view.value === 'battery',
})

const hidden = ref<string[]>([])
function toggle(id: string) {
  hidden.value = hidden.value.includes(id)
    ? hidden.value.filter((h) => h !== id)
    : [...hidden.value, id]
}
const compared = computed(() => garage.value.filter((v) => !hidden.value.includes(v.id)))
</script>

<template>
  <div class="garage">
    <div class="d-flex flex-wrap align-center ga-3 mb-2">
      <h1 class="text-headline-small me-auto">{{ t('garage.title') }}</h1>
      <GaragePeriodToggle v-if="stats" />
    </div>
    <GarageTotals :garage :period :stats />
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{
      t('garage.error')
    }}</v-alert>
    <v-progress-linear
      v-else-if="isPending"
      indeterminate
      class="mb-2"
      :aria-label="t('stats.loading')"
    />
    <GarageTiles :vehicles :labels :colors :garage :hidden :stats class="mb-6" @toggle="toggle" />
    <FeatureUnavailable v-if="!stats" feature="stats" />
    <template v-else>
      <GarageCompare
        v-if="compared.length"
        :garage="compared"
        :labels
        :colors
        :period
        :view
        class="mb-6"
      />
      <p v-else class="text-body-medium text-medium-emphasis mb-6">
        {{ t('garage.compare.nothing') }}
      </p>
      <GarageSpending :garage :labels :colors />
    </template>
  </div>
</template>
