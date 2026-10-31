<script setup lang="ts">
import { useId } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  OpeningsSummary,
  type Connection,
  type Doors,
  type Reading,
  type Windows,
} from '@/entities/vehicle'

// OpeningsLine is the doors and windows as one inset line of the vehicle's tile.
defineProps<{
  doors: Reading<Doors> | null
  windows: Reading<Windows> | null
  now: number
  connection: Pick<Connection, 'status'> & Partial<Pick<Connection, 'credential'>>
}>()

const { t } = useI18n()
const title = useId()
</script>

<template>
  <section class="openings-line" data-test="openings" :aria-labelledby="title">
    <OpeningsSummary :doors :windows :now :connection>
      <template #heading>
        <h3 :id="title" class="title">{{ t('state.openings.title') }}</h3>
      </template>
    </OpeningsSummary>
  </section>
</template>

<style scoped>
.openings-line {
  padding: 12px 16px;
  border-radius: var(--v-control-radius);
  background: rgb(var(--v-theme-surface-variant));
}
.title {
  margin: 0;
  font-size: 0.875rem;
  font-weight: 500;
  line-height: 1.4;
}
</style>
