<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useFormat } from '@/shared/lib'
import { isStale, staleAfter as defaultStaleAfter } from '../model/freshness'
import type { Connection, Reading } from '../model/Vehicle'

// ReadingProvenance says how long ago a reading was last checked and since when its value
// holds, and whether it is stale: under a value, or once under a tile whose values all
// share it. Either part can be left out, when the tile already says it.
const props = withDefaults(
  defineProps<{
    reading: Reading
    now: number
    connection: Pick<Connection, 'status'>
    staleAfter?: number
    checked?: boolean
    since?: boolean
  }>(),
  { staleAfter: defaultStaleAfter, checked: true, since: true },
)

const { t } = useI18n()
const f = useFormat()
const stale = computed(
  () =>
    props.checked &&
    isStale(props.reading.checked_at, props.now, props.connection, props.staleAfter),
)
</script>

<template>
  <span class="details d-block">
    <time v-if="checked" :datetime="reading.checked_at">{{
      t('reading.checked', { age: f.age(reading.checked_at, now) })
    }}</time>
    <template v-if="checked && since"> · </template>
    <time v-if="since" :datetime="reading.fetched_at">{{
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
