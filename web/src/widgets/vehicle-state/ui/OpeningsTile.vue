<script setup lang="ts">
import { mdiCctv } from '@mdi/js'
import { useI18n } from 'vue-i18n'
import {
  OpeningsSummary,
  type Connection,
  type Doors,
  type Reading,
  type Windows,
} from '@/entities/vehicle'
import { StatusPill } from '@/shared/ui'

// OpeningsTile is the doors and windows as a tile of their own, beside the charge's;
// Sentry Mode watching, in a pill in its title, as each tile says what of it runs.
defineProps<{
  doors: Reading<Doors> | null
  windows: Reading<Windows> | null
  now: number
  connection: Pick<Connection, 'status'> & Partial<Pick<Connection, 'credential'>>
}>()

const { t } = useI18n()
</script>

<template>
  <v-card class="tile" data-test="openings">
    <v-card-title tag="h2" class="d-flex align-center justify-space-between ga-2">
      {{ t('state.openings.title') }}
      <StatusPill v-if="doors?.value.sentry" tone="success" :icon="mdiCctv" class="sentry">
        {{ t('state.openings.sentry') }}
      </StatusPill>
    </v-card-title>
    <v-card-text>
      <OpeningsSummary :doors :windows :now :connection sentry-apart />
    </v-card-text>
  </v-card>
</template>
