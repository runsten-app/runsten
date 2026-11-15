<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { TripSummary, type PurposeFilter } from '@/entities/trip'
import { useTrips } from '@/features/browse-trips'
import { PurposeMenu } from '@/features/mark-trip-purpose'
import { useSettings } from '@/features/edit-settings'
import { periodQuery } from '@/features/filter-period'
import { useLimits } from '@/features/view-limits'
import { isApiError } from '@/shared/api'
import { groupByDay, useFormat } from '@/shared/lib'

// TripList is Urd's list of trips: newest first, by day, a page at a time, of one purpose
// or all. Each trip's purpose sits beside its link; selecting, a box before each trip
// picks it, for its purpose to be marked with others'.
const props = defineProps<{
  vehicle: string
  from?: string
  to?: string
  purpose?: PurposeFilter
  selecting?: boolean
}>()
const selected = defineModel<string[]>('selected', { default: () => [] })
const { t } = useI18n()
const f = useFormat()
const route = useRoute()
const { trips, isPending, error, hasMore, loadMore, isLoadingMore, loadMoreFailed } = useTrips(
  () => props.vehicle,
  () => props.from,
  () => props.to,
  () => props.purpose,
)

// The costs need the account's currency, and an offer that has them: else none is shown,
// not even unknown.
const { settings } = useSettings()
const { lacks } = useLimits()
const costs = computed(() => !!settings.value?.currency && !lacks('costs'))

const groups = computed(() => groupByDay(trips.value))
// The details keep the period, for the way back.
const query = computed(() => ({ ...periodQuery(route.query), purpose: props.purpose }))
const filtered = computed(() => !!(props.from || props.to || props.purpose))

function toggle(id: string, on: boolean | null) {
  selected.value = on ? [...selected.value, id] : selected.value.filter((s) => s !== id)
}

const errorMessage = computed(() => {
  if (isApiError(error.value, 'not_found')) return t('vehicle.notFound')
  if (isApiError(error.value, 'invalid_parameter')) return t('period.error.rejected')
  return t('trips.error')
})
</script>

<template>
  <v-alert v-if="error && !trips.length" type="error" variant="tonal">{{ errorMessage }}</v-alert>
  <v-progress-linear v-else-if="isPending" indeterminate :aria-label="t('trips.loading')" />
  <p v-else-if="!trips.length" class="empty text-body-large text-medium-emphasis py-6">
    {{ filtered ? t('trips.emptyPeriod') : t('trips.empty') }}
  </p>
  <div v-else class="trip-list">
    <section v-for="g in groups" :key="g.day" class="day mb-4">
      <h2 class="text-title-small text-medium-emphasis mb-1">{{ f.day(g.at) }}</h2>
      <!-- A list of links: each one in a listitem, which a list requires (its items
           have the link role); the divider inside it, where a separator is allowed. -->
      <v-list class="tile-list py-0">
        <div v-for="(trip, i) in g.items" :key="trip.id" role="listitem">
          <v-divider v-if="i > 0" />
          <div class="trip-row d-flex align-center">
            <v-checkbox-btn
              v-if="selecting"
              :model-value="selected.includes(trip.id)"
              :aria-label="t('purpose.selectTrip', { day: f.day(g.at) })"
              class="select-trip flex-grow-0 ms-2"
              @update:model-value="toggle(trip.id, $event)"
            />
            <TripSummary
              :trip
              :day="g.day"
              :costs
              :to="{ name: 'trip', params: { vehicle, id: trip.id }, query }"
              class="flex-grow-1"
            />
            <PurposeMenu :vehicle :trip class="flex-shrink-0 me-2" />
          </div>
        </div>
      </v-list>
    </section>
    <v-alert v-if="loadMoreFailed" type="warning" variant="tonal" density="compact" class="mb-3">
      {{ t('trips.moreFailed') }}
    </v-alert>
    <div class="d-flex justify-center py-2">
      <v-btn
        v-if="hasMore"
        :loading="isLoadingMore"
        variant="tonal"
        color="primary"
        class="load-more"
        @click="loadMore"
      >
        {{ t('trips.loadMore') }}
      </v-btn>
      <p v-else class="end text-body-medium text-medium-emphasis">{{ t('trips.end') }}</p>
    </div>
  </div>
</template>
