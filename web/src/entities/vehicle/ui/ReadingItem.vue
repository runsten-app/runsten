<script setup lang="ts">
import { computed } from 'vue'
import { useFormat } from '@/shared/lib'
import { TileFigure } from '@/shared/ui'
import { isStale, staleAfter as defaultStaleAfter } from '../model/freshness'
import type { Connection, Reading } from '../model/Vehicle'
import ReadingProvenance from './ReadingProvenance.vue'

// ReadingItem is one value of the state, in a <dl>: its label, the value (already
// formatted; null is unknown), how long ago it was last checked and since when it holds.
// In a tile that says these once (tile, the reading it shows them of), the value repeats
// only what differs from it. The slot adds to the value (a gauge): inside the <dd>, a
// <dl> holding only its items.
const props = withDefaults(
  defineProps<{
    label: string
    reading: Reading | null
    value: string | null
    now: number
    connection: Pick<Connection, 'status'>
    staleAfter?: number
    size?: 'hero' | 'large' | 'small'
    tile?: Reading | null
  }>(),
  { staleAfter: defaultStaleAfter, size: 'large', tile: undefined },
)

const f = useFormat()

const stale = computed(
  () =>
    props.reading !== null &&
    isStale(props.reading.checked_at, props.now, props.connection, props.staleAfter),
)
const known = computed(() => props.reading !== null && props.value !== null)
// What the value says of itself: what the tile's line does not already say, as it reads.
const own = computed(() => {
  const r = props.reading
  if (!r) return null
  const t = props.tile
  if (t === undefined) return { checked: true, since: true }
  const checked = !t || f.age(r.checked_at, props.now) !== f.age(t.checked_at, props.now)
  const since = !t || f.dateTime(r.fetched_at, props.now) !== f.dateTime(t.fetched_at, props.now)
  return checked || since ? { checked, since } : null
})
</script>

<template>
  <div class="reading" :class="{ stale }">
    <dt class="label">{{ label }}</dt>
    <dd>
      <TileFigure
        class="value"
        :size
        :text="known ? (value ?? '') : f.unknown()"
        :unknown="!known"
      />
      <slot />
      <ReadingProvenance
        v-if="reading && own"
        :reading
        :now
        :connection
        :stale-after="staleAfter"
        :checked="own.checked"
        :since="own.since"
        class="mt-1"
      />
    </dd>
  </div>
</template>

<style scoped>
.reading {
  min-width: 0;
}
.label {
  font-size: 0.8125rem;
  color: rgb(var(--v-theme-text-secondary));
  margin-bottom: 4px;
}
dd {
  margin: 0;
}
/* A stale value in the secondary color, which keeps 4.5:1: never faded further. */
.stale .value {
  color: rgb(var(--v-theme-text-secondary));
}
</style>
