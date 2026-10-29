<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ChargeInProgressSummary, ChargeSummary } from '@/entities/charge'
import { collectorStoppedAfter } from '@/entities/vehicle'
import { useChargeInProgress, useCharges } from '@/features/browse-charges'
import { periodQuery } from '@/features/filter-period'
import { isApiError } from '@/shared/api'
import { groupByDay, useFormat, useNow } from '@/shared/lib'

// ChargeList is Urd's list of charges: newest first, by day, a page at a time. The charge
// in progress, no event of the list, comes first, while the period reaches the present.
const props = defineProps<{ vehicle: string; from?: string; to?: string }>()
const { t } = useI18n()
const f = useFormat()
const route = useRoute()
const now = useNow()
const { charges, isPending, error, hasMore, loadMore, isLoadingMore, loadMoreFailed } = useCharges(
  () => props.vehicle,
  () => props.from,
  () => props.to,
)
const { charge: open } = useChargeInProgress(() => props.vehicle)
const live = computed(() => {
  const c = open.value
  return c && (!props.to || Date.parse(props.to) > Date.parse(c.start.after)) ? c : null
})
const stale = computed(
  () => !!live.value && now.value - Date.parse(live.value.read_at) > collectorStoppedAfter,
)

// A charge that resumes is still listed as it ended until the collector derives it
// again: the one in progress stands for it.
const groups = computed(() => groupByDay(charges.value.filter((c) => c.id !== live.value?.id)))
// The details keep the period, for the way back.
const query = computed(() => periodQuery(route.query))
const filtered = computed(() => !!(props.from || props.to))

const errorMessage = computed(() => {
  if (isApiError(error.value, 'not_found')) return t('vehicle.notFound')
  if (isApiError(error.value, 'invalid_parameter')) return t('period.error.rejected')
  return t('charges.error')
})
</script>

<template>
  <section v-if="live" class="in-progress mb-4">
    <h2 class="text-title-small text-medium-emphasis mb-1">{{ t('chargeInProgress.heading') }}</h2>
    <v-list class="tile-list py-0">
      <div role="listitem">
        <ChargeInProgressSummary
          :charge="live"
          :stale
          :now
          :to="{ name: 'charge', params: { vehicle, id: live.id }, query }"
        />
      </div>
    </v-list>
  </section>
  <v-alert v-if="error && !charges.length" type="error" variant="tonal">{{ errorMessage }}</v-alert>
  <v-progress-linear v-else-if="isPending" indeterminate :aria-label="t('charges.loading')" />
  <p v-else-if="!groups.length && !live" class="empty text-body-large text-medium-emphasis py-6">
    {{ filtered ? t('charges.emptyPeriod') : t('charges.empty') }}
  </p>
  <div v-else-if="groups.length" class="charge-list">
    <section v-for="g in groups" :key="g.day" class="day mb-4">
      <h2 class="text-title-small text-medium-emphasis mb-1">{{ f.day(g.at) }}</h2>
      <!-- A list of links: each one in a listitem, which a list requires (its items
           have the link role); the divider inside it, where a separator is allowed. -->
      <v-list class="tile-list py-0">
        <div v-for="(charge, i) in g.items" :key="charge.id" role="listitem">
          <v-divider v-if="i > 0" />
          <ChargeSummary
            :charge
            :day="g.day"
            :to="{ name: 'charge', params: { vehicle, id: charge.id }, query }"
          />
        </div>
      </v-list>
    </section>
    <v-alert v-if="loadMoreFailed" type="warning" variant="tonal" density="compact" class="mb-3">
      {{ t('charges.moreFailed') }}
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
        {{ t('charges.loadMore') }}
      </v-btn>
      <p v-else class="end text-body-medium text-medium-emphasis">{{ t('charges.end') }}</p>
    </div>
  </div>
</template>
