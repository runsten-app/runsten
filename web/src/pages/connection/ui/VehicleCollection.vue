<script setup lang="ts">
import {
  mdiAccountAlert,
  mdiAlertCircle,
  mdiCheckCircle,
  mdiClockOutline,
  mdiGaugeFull,
  mdiKeyAlert,
  mdiMinusCircle,
  mdiPauseCircle,
} from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { collectionState, type CollectionState, type Vehicle } from '@/entities/vehicle'
import { useCollectionText } from '@/features/watch-collection'
import { useFormat } from '@/shared/lib'
import { FactItem } from '@/shared/ui'

// VehicleCollection is how the collector reads one vehicle: a sentence first, then what
// it rests on. Every time is the collector's, as of its latest pass. unread: the
// account's offer leaves the vehicle out.
const props = defineProps<{ vehicle: Vehicle; label: string; now: number; unread?: boolean }>()
const { t } = useI18n()
const f = useFormat()
const text = useCollectionText()

const state = computed(() => collectionState(props.vehicle, props.now, props.unread))
const c = computed(() => props.vehicle.collection)
const looks: Record<CollectionState['kind'], { icon: string; color: string }> = {
  unread: { icon: mdiMinusCircle, color: 'info' },
  reauth: { icon: mdiAccountAlert, color: 'warning' },
  key: { icon: mdiKeyAlert, color: 'warning' },
  waiting: { icon: mdiClockOutline, color: 'info' },
  stopped: { icon: mdiAlertCircle, color: 'error' },
  paused: { icon: mdiPauseCircle, color: 'warning' },
  quota: { icon: mdiGaugeFull, color: 'warning' },
  reading: { icon: mdiCheckCircle, color: 'success' },
}
const modeLabels = computed(() => ({
  parked: t('state.mode.parked'),
  driving: t('state.mode.driving'),
  charging: t('state.mode.charging'),
}))
const quota = computed(() => (c.value?.quota ?? []).filter((q) => Date.parse(q.until) > props.now))
const nextRead = computed(() => {
  const next = c.value?.next_read_at
  if (!c.value) return f.unknown()
  if (!next)
    return state.value.kind === 'key'
      ? t('connection.facts.noneDueKey')
      : t('connection.facts.noneDue')
  return Date.parse(next) <= props.now ? t('connection.facts.due') : f.dateTime(next, props.now)
})
const failure = computed(() => {
  const l = c.value?.last_failure
  if (!l) return null
  const where = [l.endpoint, l.status === null ? null : `HTTP ${l.status}`].filter(Boolean)
  return {
    value: `${text.failure(l.kind)} · ${f.dateTime(l.at, props.now)}`,
    where: where.join(' · '),
  }
})
</script>

<template>
  <v-card class="vehicle-collection mb-4">
    <v-card-title tag="h3" class="text-title-large">{{ label }}</v-card-title>
    <v-card-text>
      <p class="state d-flex align-center ga-2 text-body-large mb-4">
        <v-icon :icon="looks[state.kind].icon" :color="looks[state.kind].color" />
        <span>{{ text.state(state, now) }}</span>
      </p>
      <dl class="facts">
        <FactItem
          :label="t('connection.facts.mode')"
          :value="c ? modeLabels[c.mode] : f.unknown()"
        />
        <FactItem
          :label="t('connection.facts.passedAt')"
          :value="c ? f.age(c.passed_at, now) : t('connection.facts.never')"
        />
        <FactItem
          :label="t('connection.facts.readAt')"
          :value="c?.read_at ? f.dateTime(c.read_at, now) : f.unknown()"
        />
        <FactItem :label="t('connection.facts.nextReadAt')" :value="nextRead" />
        <FactItem
          v-if="quota.length"
          :label="t('connection.facts.quota')"
          :value="
            quota
              .map((q) =>
                t('connection.facts.quotaUntil', {
                  api: text.api(q.api),
                  time: f.dateTime(q.until, now),
                }),
              )
              .join(', ')
          "
        />
        <FactItem
          :label="t('connection.facts.lastFailure')"
          :value="failure?.value ?? t('connection.facts.noFailure')"
        >
          <p v-if="failure?.where" class="failure-where text-body-medium text-medium-emphasis">
            {{ failure.where }}
          </p>
        </FactItem>
      </dl>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.facts {
  display: grid;
  gap: 1rem;
  grid-template-columns: repeat(auto-fill, minmax(14rem, 1fr));
}
</style>
