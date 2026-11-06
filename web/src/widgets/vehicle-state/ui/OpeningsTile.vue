<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import {
  OpeningsSummary,
  type Connection,
  type Doors,
  type Reading,
  type Windows,
} from '@/entities/vehicle'

// OpeningsTile is the doors and windows as a tile of their own, under the charge's.
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
    <v-card-title tag="h2">{{ t('state.openings.title') }}</v-card-title>
    <v-card-text>
      <OpeningsSummary :doors :windows :now :connection />
    </v-card-text>
  </v-card>
</template>
