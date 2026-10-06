<script setup lang="ts">
import { mdiDelete, mdiLinkVariant } from '@mdi/js'
import { computed, nextTick, ref, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import type { OrphanCost } from '@/entities/charge'
import { currencyUnit, useFormat } from '@/shared/lib'
import { FactItem } from '@/shared/ui'
import { useOrphanCosts } from '../composables/useOrphanCosts'
import AttachOrphanDialog from './AttachOrphanDialog.vue'
import DeleteOrphanDialog from './DeleteOrphanDialog.vue'

// OrphanCosts is the section of the settings for the entered costs no charge takes any
// more, shown only when there is one: each with its window, amount and note, to attach
// to a charge or delete. Never lost, never counted: the statistics show them apart. The
// dialogs are the section's, not the cards': a card goes once its cost is done with.
const { t } = useI18n()
const f = useFormat()
const { orphans, error } = useOrphanCosts()
const status = ref('')
const heading = ref<HTMLElement | null>(null)
const attaching = ref<OrphanCost | null>(null)
const deleting = ref<OrphanCost | null>(null)
// The button that opened a dialog, for the focus when it closes without a change.
let opener = ''

// Once shown, the section stays when the last one is gone: its dialogs, status and focus
// are there.
const had = ref(false)
watchEffect(() => {
  if (orphans.value?.length) had.value = true
})
const shown = computed(() => had.value || !!error.value)

function amount(o: OrphanCost): string {
  return o.currency
    ? f.amount(o.amount_minor, currencyUnit(o.currency))
    : t('orphans.noCurrency', { n: o.amount_minor })
}

function openAttach(o: OrphanCost) {
  status.value = ''
  opener = `orphan-attach-${o.id}`
  attaching.value = o
}
function openDelete(o: OrphanCost) {
  status.value = ''
  opener = `orphan-delete-${o.id}`
  deleting.value = o
}

// Cancelled, the focus goes back to the button that opened the dialog. Done, its card is
// gone: to the section's heading, and the status says what happened.
async function cancelled() {
  attaching.value = null
  deleting.value = null
  await nextTick()
  document.getElementById(opener)?.focus()
}
async function done(said: string) {
  attaching.value = null
  deleting.value = null
  await nextTick()
  heading.value?.focus()
  status.value = said
}
</script>

<template>
  <section v-if="shown" class="orphan-costs mt-8" aria-labelledby="settings-orphans-title">
    <h2 id="settings-orphans-title" ref="heading" tabindex="-1" class="text-title-large mb-2">
      {{ t('orphans.title') }}
    </h2>
    <p class="text-body-medium mb-4">{{ t('orphans.help') }}</p>
    <v-alert v-if="error" type="error" variant="tonal">{{ t('orphans.error') }}</v-alert>
    <p v-else-if="!orphans?.length" class="none text-body-medium">{{ t('orphans.none') }}</p>
    <v-card v-for="o in orphans" :key="o.id" class="orphan mb-3">
      <v-card-title tag="h3" class="text-title-medium text-wrap">
        {{ t('orphans.window', { window: f.bounds(o.window) }) }}
      </v-card-title>
      <v-card-text>
        <dl class="facts">
          <FactItem :label="t('orphans.amount')" :value="amount(o)" />
          <FactItem
            :label="t('orphans.energy')"
            :value="o.energy_kwh === null ? t('orphans.noEnergy') : f.quantity(o.energy_kwh, 'kWh')"
          />
          <FactItem :label="t('orphans.note')" :value="o.note ?? t('orphans.noNote')" />
        </dl>
      </v-card-text>
      <v-card-actions class="flex-wrap ga-2 px-4 pb-4">
        <v-btn
          :id="`orphan-attach-${o.id}`"
          :prepend-icon="mdiLinkVariant"
          :aria-label="t('orphans.attach.label', { cost: amount(o) })"
          variant="tonal"
          color="primary"
          class="attach-orphan"
          @click="openAttach(o)"
        >
          {{ t('orphans.attach.button') }}
        </v-btn>
        <v-btn
          :id="`orphan-delete-${o.id}`"
          :prepend-icon="mdiDelete"
          :aria-label="t('orphans.delete.label', { cost: amount(o) })"
          variant="text"
          color="error"
          class="delete-orphan"
          @click="openDelete(o)"
        >
          {{ t('orphans.delete.button') }}
        </v-btn>
      </v-card-actions>
    </v-card>
    <p role="status" class="status text-body-medium">{{ status }}</p>
    <AttachOrphanDialog
      :orphan="attaching"
      @cancel="cancelled"
      @attached="done(t('orphans.attached'))"
    />
    <DeleteOrphanDialog
      :orphan="deleting"
      :amount="deleting ? amount(deleting) : ''"
      @cancel="cancelled"
      @deleted="done(t('orphans.deleted'))"
    />
  </section>
</template>

<style scoped>
.facts {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(12rem, 1fr));
  gap: 1rem;
}
h2:focus-visible {
  outline: 2px solid rgb(var(--v-theme-primary));
  outline-offset: 2px;
}
</style>
