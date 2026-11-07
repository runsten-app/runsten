<script setup lang="ts">
import { mdiCctv } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Health } from '@/entities/health'
import {
  OpeningsSummary,
  type Connection,
  type Doors,
  type Reading,
  type Windows,
} from '@/entities/vehicle'
import { StatusPill } from '@/shared/ui'
import TyresPart from './TyresPart.vue'

// OpeningsTile is the car seen from above, beside the charge: its doors and windows, then
// its tyres, as far as it tells them; Sentry Mode watching, in a pill in its title, as
// each tile says what of it runs.
const props = defineProps<{
  doors: Reading<Doors> | null
  windows: Reading<Windows> | null
  now: number
  connection: Pick<Connection, 'status'> & Partial<Pick<Connection, 'credential'>>
  health: Health | null
}>()

const { t } = useI18n()
const openings = computed(() => !!(props.doors || props.windows))
const tyres = computed(() => (props.health?.tyres ? props.health : null))
const title = computed(() =>
  !tyres.value
    ? t('state.openings.title')
    : openings.value
      ? t('state.openings.titleWithTyres')
      : t('health.tyres.title'),
)
</script>

<template>
  <v-card class="tile" data-test="openings">
    <v-card-title tag="h2" class="d-flex align-center justify-space-between ga-2">
      {{ title }}
      <StatusPill v-if="doors?.value.sentry" tone="success" :icon="mdiCctv" class="sentry">
        {{ t('state.openings.sentry') }}
      </StatusPill>
    </v-card-title>
    <v-card-text>
      <OpeningsSummary v-if="openings" :doors :windows :now :connection sentry-apart />
      <TyresPart v-if="tyres" :health="tyres" :class="{ part: openings }" />
    </v-card-text>
  </v-card>
</template>

<style scoped>
.part {
  margin-top: 16px;
  padding-top: 16px;
  border-top: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
}
</style>
