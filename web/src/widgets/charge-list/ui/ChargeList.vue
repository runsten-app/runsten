<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ChargeSummary } from '@/entities/charge'
import { useCharges } from '@/features/browse-charges'
import { periodQuery } from '@/features/filter-period'
import { isApiError } from '@/shared/api'
import { groupByDay, useFormat } from '@/shared/lib'

// ChargeList is Urd's list of charges: newest first, by day, a page at a time.
const props = defineProps<{ vehicle: string; from?: string; to?: string }>()
const { t } = useI18n()
const f = useFormat()
const route = useRoute()
const { charges, isPending, error, hasMore, loadMore, isLoadingMore, loadMoreFailed } = useCharges(
  () => props.vehicle,
  () => props.from,
  () => props.to,
)

const groups = computed(() => groupByDay(charges.value))
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
  <v-alert v-if="error && !charges.length" type="error" variant="tonal">{{ errorMessage }}</v-alert>
  <v-progress-linear v-else-if="isPending" indeterminate :aria-label="t('charges.loading')" />
  <p v-else-if="!charges.length" class="empty text-body-large text-medium-emphasis py-6">
    {{ filtered ? t('charges.emptyPeriod') : t('charges.empty') }}
  </p>
  <div v-else class="charge-list">
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
