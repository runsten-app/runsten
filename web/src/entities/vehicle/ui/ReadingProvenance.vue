<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useFormat } from '@/shared/lib'
import { isStale, staleAfter as defaultStaleAfter, type Headline } from '../model/freshness'
import type { Connection, Reading } from '../model/Vehicle'

// ReadingProvenance says how long ago a reading was last checked and since when its value
// holds, and whether it is stale: under a value, or once under a tile whose values all
// share it. Either part can be left out, when the tile already says it. Under a headline
// that says both for the whole page, it says only what differs from it: its check when
// stale while the headline's is not, its time when not the headline's; else nothing.
const props = withDefaults(
  defineProps<{
    reading: Reading
    now: number
    connection: Pick<Connection, 'status'> & Partial<Pick<Connection, 'credential'>>
    staleAfter?: number
    checked?: boolean
    since?: boolean
    headline?: Headline
  }>(),
  { staleAfter: defaultStaleAfter, checked: true, since: true, headline: undefined },
)

const { t } = useI18n()
const f = useFormat()
const readingStale = computed(() =>
  isStale(props.reading.checked_at, props.now, props.connection, props.staleAfter),
)
const showChecked = computed(() =>
  props.headline ? readingStale.value && !props.headline.stale : props.checked,
)
// The headline's time is ours (a check, the car falling asleep) or the car's (its late
// measure): the reading's is the same if either of its own reads alike.
const showSince = computed(() => {
  const h = props.headline
  if (!h) return props.since
  if (!h.at) return true
  const at = f.dateTime(h.at, props.now)
  const { fetched_at, reported_at } = props.reading
  return ![fetched_at, reported_at].some((t) => t && f.dateTime(t, props.now) === at)
})
const stale = computed(() => showChecked.value && readingStale.value)
</script>

<template>
  <span v-if="showChecked || showSince" class="details d-block">
    <time v-if="showChecked" :datetime="reading.checked_at">{{
      t('reading.checked', { age: f.age(reading.checked_at, now) })
    }}</time>
    <template v-if="showChecked && showSince"> · </template>
    <time v-if="showSince" :datetime="reading.fetched_at">{{
      t('reading.since', { time: f.dateTime(reading.fetched_at, now) })
    }}</time>
    <v-chip v-if="stale" size="x-small" variant="outlined" class="ms-1">{{
      t('reading.stale')
    }}</v-chip>
  </span>
</template>

<style scoped>
.details {
  font-size: 0.75rem;
  line-height: 1.4;
  color: rgb(var(--v-theme-text-secondary));
}
</style>
