<script setup lang="ts">
import { mdiDelete } from '@mdi/js'
import { computed, nextTick, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { VehicleImport } from '@/entities/vehicle-import'
import { useFormat } from '@/shared/lib'
import { ImportedBadge } from '@/shared/ui'
import { useDeleteImport, useImports } from '../composables/useImports'
import AximoteImport from './AximoteImport.vue'

// VehicleImports imports a vehicle's history from Aximote, then lists the trips and
// charges imported into it from other applications, by source, and deletes one
// source's: what Runsten derived stays.
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

const titleId = 'vehicle-imports-title'
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
  <section class="vehicle-imports mb-6" :aria-labelledby="titleId">
    <h2 :id="titleId" class="text-title-large mb-2" tabindex="-1">{{ t('imports.title') }}</h2>
    <p class="text-body-medium mb-4">{{ t('imports.help') }}</p>
    <AximoteImport :vehicle />
    <h3 class="text-title-medium mb-2">{{ t('imports.listTitle') }}</h3>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">
      {{ t('imports.error') }}
    </v-alert>
    <v-list v-else-if="imports?.length" class="tile-list py-0 mb-4">
      <div v-for="(i, n) in imports" :key="i.source" role="listitem" class="import">
        <v-divider v-if="n > 0" />
        <div class="d-flex flex-wrap align-center ga-2 px-4 py-3">
          <div class="flex-grow-1">
            <ImportedBadge :source="i.source" class="mb-1" />
            <p class="text-body-medium counts">{{ counts(i) }}</p>
            <p class="text-body-medium text-secondary span">{{ span(i) }}</p>
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
    <p v-else class="none text-body-medium mb-4">{{ t('imports.none') }}</p>

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
    <p role="status" class="status text-body-medium mt-2">{{ status }}</p>
  </section>
</template>
