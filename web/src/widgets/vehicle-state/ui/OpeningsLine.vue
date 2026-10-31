<script setup lang="ts">
import { mdiLock, mdiLockOpenVariant } from '@mdi/js'
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  openings,
  ReadingProvenance,
  reportLateAfter,
  type Connection,
  type Doors,
  type OpeningName,
  type Reading,
  type Windows,
} from '@/entities/vehicle'
import { useFormat } from '@/shared/lib'
import { StatusPill } from '@/shared/ui'

// OpeningsLine says in one line whether the vehicle is locked and what of it is open,
// for a source that tells it: "Locked · All closed", else the open ones named. Its times
// are its own (the doors are read apart from the rest), and the car's own measure when it
// is late: a Polestar uploads them hours after.
const props = defineProps<{
  doors: Reading<Doors> | null
  windows: Reading<Windows> | null
  now: number
  connection: Pick<Connection, 'status'> & Partial<Pick<Connection, 'credential'>>
}>()

const { t } = useI18n()
const f = useFormat()
const title = useId()

const summary = computed(() => openings(props.doors?.value ?? null, props.windows?.value ?? null))
// Each name is written out, so that the i18n lint sees every key in use.
const names = computed<Record<OpeningName, string>>(() => ({
  frontLeftDoor: t('state.openings.names.frontLeftDoor'),
  frontRightDoor: t('state.openings.names.frontRightDoor'),
  rearLeftDoor: t('state.openings.names.rearLeftDoor'),
  rearRightDoor: t('state.openings.names.rearRightDoor'),
  hood: t('state.openings.names.hood'),
  tailgate: t('state.openings.names.tailgate'),
  tankLid: t('state.openings.names.tankLid'),
  frontLeftWindow: t('state.openings.names.frontLeftWindow'),
  frontRightWindow: t('state.openings.names.frontRightWindow'),
  rearLeftWindow: t('state.openings.names.rearLeftWindow'),
  rearRightWindow: t('state.openings.names.rearRightWindow'),
  sunroof: t('state.openings.names.sunroof'),
}))
const listed = (state: 'open' | 'ajar') => {
  const words = summary.value.open.filter((o) => o.state === state).map((o) => names.value[o.name])
  return words.length ? f.list(words) : null
}
// What is open, then what is ajar; all closed only when the vehicle told some opening.
const facts = computed(() => {
  const open = listed('open')
  const ajar = listed('ajar')
  const out: string[] = []
  if (open) out.push(t('state.openings.open', { list: open }))
  if (ajar) out.push(t('state.openings.ajar', { list: ajar }))
  if (!out.length && summary.value.told) out.push(t('state.openings.allClosed'))
  return out
})
const sentry = computed(() => !!props.doors?.value.sentry)

// The reading whose times the line shows: the doors', which tell the lock.
const reading = computed(() => props.doors ?? props.windows)
const measured = computed(() => {
  const at = reading.value?.reported_at
  return at && props.now - Date.parse(at) > reportLateAfter ? at : null
})
</script>

<template>
  <section class="openings-line" data-test="openings" :aria-labelledby="title">
    <div class="d-flex flex-wrap align-center row">
      <h3 :id="title" class="title">{{ t('state.openings.title') }}</h3>
      <StatusPill
        v-if="summary.locked !== null"
        :icon="summary.locked ? mdiLock : mdiLockOpenVariant"
        class="lock"
      >
        {{ summary.locked ? t('state.openings.locked') : t('state.openings.unlocked') }}
      </StatusPill>
      <span v-for="fact in facts" :key="fact" class="fact">{{ fact }}</span>
      <span v-if="sentry" class="fact sentry">{{ t('state.openings.sentry') }}</span>
    </div>
    <div v-if="reading" class="mt-2">
      <ReadingProvenance :reading="reading" :now="now" :connection="connection" />
      <time v-if="measured" :datetime="measured" class="details measured d-block">{{
        t('state.lastReported', { time: f.dateTime(measured, now) })
      }}</time>
    </div>
  </section>
</template>

<style scoped>
.openings-line {
  padding: 12px 16px;
  border-radius: var(--v-control-radius);
  background: rgb(var(--v-theme-surface-variant));
}
.row {
  gap: 8px 16px;
}
.title {
  margin: 0;
  font-size: 0.875rem;
  font-weight: 500;
  line-height: 1.4;
}
.fact {
  font-size: 0.875rem;
  font-weight: 500;
}
.details {
  font-size: 0.75rem;
  line-height: 1.4;
  color: rgb(var(--v-theme-text-secondary));
}
</style>
