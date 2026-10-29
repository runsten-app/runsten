<script setup lang="ts">
import { mdiArrowLeft } from '@mdi/js'
import { computed, defineAsyncComponent } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { useChargeInProgress } from '@/features/browse-charges'
import { periodQuery } from '@/features/filter-period'
import { AppShell } from '@/widgets/app-shell'
import { ChargeDetails } from '@/widgets/charge-details'
import { ChargeInProgressDetails } from '@/widgets/charge-in-progress'

// The curve comes on demand, with Chart.js: the details show without waiting for it.
const ChargeCurve = defineAsyncComponent(() =>
  import('@/widgets/charge-curve').then((m) => m.ChargeCurve),
)

const props = defineProps<{ vehicle: string; id: string }>()
const { t } = useI18n()
const route = useRoute()
// Back to the list as it was left: the same period.
const list = computed(() => ({
  name: 'charges',
  params: { vehicle: props.vehicle },
  query: periodQuery(route.query),
}))

// A charge in progress has the ID it will have once over: the same page follows it live,
// then shows it derived, without leaving.
const { charge: open, isPending } = useChargeInProgress(() => props.vehicle)
const live = computed(() => (open.value?.id === props.id ? open.value : null))
</script>

<template>
  <AppShell :vehicle="vehicle">
    <v-btn
      :to="list"
      :active="false"
      :prepend-icon="mdiArrowLeft"
      variant="text"
      class="back mb-2 px-2"
    >
      {{ t('charge.back') }}
    </v-btn>
    <v-progress-linear v-if="isPending" indeterminate :aria-label="t('charge.loading')" />
    <ChargeInProgressDetails v-else-if="live" :charge="live" />
    <ChargeDetails v-else :id="id" :vehicle="vehicle" />
    <ChargeCurve v-if="!isPending" :id="id" :vehicle="vehicle" :live class="mt-4" />
  </AppShell>
</template>
