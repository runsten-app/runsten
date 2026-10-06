<script setup lang="ts">
import { mdiArrowLeft } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { periodQuery } from '@/features/filter-period'
import { AppShell } from '@/widgets/app-shell'
import { TripDetails } from '@/widgets/trip-details'

const props = defineProps<{ vehicle: string; id: string }>()
const { t } = useI18n()
const route = useRoute()
// Back to the list as it was left: the same period.
const list = computed(() => ({
  name: 'trips',
  params: { vehicle: props.vehicle },
  query: periodQuery(route.query),
}))
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
      {{ t('trip.back') }}
    </v-btn>
    <TripDetails :id="id" :vehicle="vehicle" />
  </AppShell>
</template>
