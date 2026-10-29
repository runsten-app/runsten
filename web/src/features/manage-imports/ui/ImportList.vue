<script setup lang="ts">
import { mdiDelete } from '@mdi/js'
import { computed, nextTick, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { VehicleImport } from '@/entities/vehicle-import'
import { useFormat } from '@/shared/lib'
import { ImportedBadge } from '@/shared/ui'
import { useDeleteImport, useImports } from '../composables/useImports'

// ImportList lists the trips and charges imported into a vehicle from other applications,
// by source, and deletes one source's: what Runsten derived stays. A tile of its own,
// before the imports themselves (ImportSources): what the vehicle holds comes first.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const f = useFormat()
const { imports, error } = useImports(() => props.vehicle)
const {
  deleteImport,
  isPending: deleting,
  error: deleteError,
  reset,
} = useDeleteImport(() => props.vehicle)

const titleId = 'import-list-title'
const status = ref('')
const deleted = ref<VehicleImport | null>(null)
const confirming = computed({
  get: () => deleted.value !== null,
  set: (v) => {
    if (!v) deleted.value = null
  },
})

const counts = (i: VehicleImport) =>
  [
    t('imports.trips', { n: i.trips }, i.trips),
    t('imports.charges', { n: i.charges }, i.charges),
  ].join(' · ')
const span = (i: VehicleImport) =>
  t('imports.span', { from: f.day(i.started_from), to: f.day(i.started_to) })

function ask(i: VehicleImport) {
  status.value = ''
  reset()
  deleted.value = i
}

async function confirm() {
  const i = deleted.value
  if (!i) return
  try {
    await deleteImport(i.source)
    deleted.value = null
    await nextTick()
    document.getElementById(titleId)?.focus()
    status.value = t('imports.delete.done', { what: f.importedFrom(i.source) })
  } catch {
    // Shown from the error, in the dialog.
  }
}
</script>

<template>
  <!-- Inert behind the dialog, which is teleported out of it: a modal leaves nothing
       behind it to reach, nor to read through its scrim. -->
  <v-card
    tag="section"
    class="import-list"
    :aria-labelledby="titleId"
    :inert="confirming || undefined"
  >
    <v-card-title :id="titleId" tag="h3" class="text-title-large" tabindex="-1">
      {{ t('imports.listTitle') }}
    </v-card-title>
    <v-alert v-if="error" type="error" variant="tonal" class="mx-4 mb-4">
      {{ t('imports.error') }}
    </v-alert>
    <!-- A list of rows on the tile: each in a listitem, the divider inside it. -->
    <v-list v-else-if="imports?.length" class="py-0" bg-color="transparent">
      <div v-for="(i, n) in imports" :key="i.source" role="listitem" class="import">
        <v-divider v-if="n > 0" />
        <div class="d-flex flex-wrap align-center ga-2 px-4 py-3">
          <div class="flex-grow-1">
            <ImportedBadge :source="i.source" class="mb-1" />
            <p class="text-body-medium counts">{{ counts(i) }}</p>
            <p class="text-body-medium text-medium-emphasis span">{{ span(i) }}</p>
          </div>
          <v-btn
            :prepend-icon="mdiDelete"
            variant="text"
            color="error"
            class="delete"
            :aria-label="t('imports.delete.label', { what: f.importedFrom(i.source) })"
            @click="ask(i)"
          >
            {{ t('imports.delete.button') }}
          </v-btn>
        </div>
      </div>
    </v-list>
    <v-card-text v-else class="pt-0">
      <p class="none text-body-medium">{{ t('imports.none') }}</p>
    </v-card-text>

    <v-dialog v-model="confirming" max-width="28rem" aria-labelledby="delete-import-title">
      <v-card v-if="deleted">
        <v-card-title id="delete-import-title" tag="h2" class="text-title-large text-wrap">
          {{ t('imports.delete.title') }}
        </v-card-title>
        <v-card-text>
          <p class="text-body-medium">
            {{
              t('imports.delete.text', {
                what: f.importedFrom(deleted.source),
                counts: counts(deleted),
              })
            }}
          </p>
          <v-alert v-if="deleteError" type="error" variant="tonal" class="mt-4">
            {{ t('imports.delete.error') }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="confirming = false">{{ t('imports.cancel') }}</v-btn>
          <v-btn
            color="error"
            variant="flat"
            :loading="deleting"
            class="confirm-delete"
            @click="confirm"
          >
            {{ t('imports.delete.confirm') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
    <!-- Always in the page, padded only when it tells something. -->
    <p role="status" class="status text-body-medium" :class="{ 'px-4 pb-4': status }">
      {{ status }}
    </p>
  </v-card>
</template>
