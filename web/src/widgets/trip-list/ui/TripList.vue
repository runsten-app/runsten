<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { TripSummary } from '@/entities/trip'
import { useTrips } from '@/features/browse-trips'
import { periodQuery } from '@/features/filter-period'
import { isApiError } from '@/shared/api'
import { groupByDay, useFormat } from '@/shared/lib'

// TripList is Urd's list of trips: newest first, by day, a page at a time.
const props = defineProps<{ vehicle: string; from?: string; to?: string }>()
const { t } = useI18n()
const f = useFormat()
const route = useRoute()
const { trips, isPending, error, hasMore, loadMore, isLoadingMore, loadMoreFailed } = useTrips(
  () => props.vehicle,
  () => props.from,
  () => props.to,
)

const groups = computed(() => groupByDay(trips.value))
// The details keep the period, for the way back.
const query = computed(() => periodQuery(route.query))
const filtered = computed(() => !!(props.from || props.to))

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
          <TripSummary
            :trip
            :day="g.day"
            :to="{ name: 'trip', params: { vehicle, id: trip.id }, query }"
          />
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
