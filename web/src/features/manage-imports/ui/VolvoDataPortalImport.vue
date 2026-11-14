<script setup lang="ts">
import { mdiFileUploadOutline, mdiOpenInNew } from '@mdi/js'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { FileImport } from '@/entities/vehicle-import'
import { isApiError } from '@/shared/api'
import { useFormat } from '@/shared/lib'
import { useImportVolvoDataPortal } from '../composables/useImportJob'

// VolvoDataPortalImport imports the files an owner in the EU gets from the Volvo Data
// Portal under the Data Act: dozens of ZIPs to download one by one, trips and waypoints
// mixed. Its button opens the browser's picker, and the files chosen are imported at
// once: choosing them is the confirmation. The report shows until the next import.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const f = useFormat()
const { importFiles, isPending, error, reset } = useImportVolvoDataPortal(() => props.vehicle)

const picker = ref<HTMLInputElement | null>(null)
const status = ref('')
// The latest report: another import clears the mutation, not what it did.
const report = ref<FileImport | null>(null)

const errorText = computed(() => {
  const e = error.value
  if (!e) return null
  if (isApiError(e, 'invalid_file')) return t('imports.portal.error.file')
  if (isApiError(e, 'invalid_body')) return t('imports.portal.error.tooLarge')
  if (isApiError(e, 'network')) return t('imports.portal.error.network')
  return t('imports.portal.error.internal')
})

function choose() {
  picker.value?.click()
}

async function picked(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  // Cleared, the same files chosen again are imported again.
  input.value = ''
  if (!files.length) return
  status.value = ''
  report.value = null
  reset()
  try {
    report.value = await importFiles(files)
    status.value = t('imports.portal.done')
  } catch {
    // The error shows under the button.
  }
}

const lines = computed(() => {
  const r = report.value
  if (!r) return []
  const out = [t('imports.portal.report.imported', { n: r.imported }, r.imported)]
  if (r.skipped > 0)
    out.push(t('imports.portal.report.skipped', { n: r.skipped, date: f.day(r.cutoff) }, r.skipped))
  if (r.invalid > 0) out.push(t('imports.portal.report.invalid', { n: r.invalid }, r.invalid))
  return out
})
</script>

<template>
  <div class="portal-import">
    <h4 class="text-title-medium mb-2">
      {{ t('imports.portal.title') }}
      <v-chip size="small" variant="tonal" color="primary" class="ms-2 beta">
        {{ t('imports.portal.beta') }}
      </v-chip>
    </h4>
    <p class="text-body-medium mb-2">{{ t('imports.portal.intro') }}</p>
    <p class="text-body-medium mb-4">
      <a
        href="https://data.volvocars.com/"
        target="_blank"
        rel="noopener noreferrer"
        class="portal-link text-primary"
      >
        {{ t('imports.portal.link') }}<v-icon :icon="mdiOpenInNew" size="x-small" class="ms-1" />
        <span class="d-sr-only">{{ t('imports.portal.newTab') }}</span>
      </a>
    </p>

    <v-alert
      v-if="lines.length"
      type="success"
      variant="tonal"
      :title="t('imports.portal.done')"
      class="import-report mb-4"
    >
      <p v-for="line in lines" :key="line" class="text-body-medium">{{ line }}</p>
    </v-alert>
    <v-alert
      v-if="errorText"
      type="error"
      variant="tonal"
      :title="t('imports.portal.error.summary')"
      class="import-error mb-4"
    >
      <p class="text-body-medium">{{ errorText }}</p>
    </v-alert>

    <input
      ref="picker"
      type="file"
      multiple
      accept=".zip,application/zip,.csv,text/csv,.json,application/json"
      class="picker d-none"
      tabindex="-1"
      aria-hidden="true"
      @change="picked"
    />
    <v-btn
      id="portal-import-toggle"
      :prepend-icon="mdiFileUploadOutline"
      :loading="isPending"
      variant="tonal"
      class="toggle"
      aria-describedby="portal-files-hint"
      @click="choose"
    >
      {{ t('imports.portal.open') }}
    </v-btn>
    <p id="portal-files-hint" class="hint text-body-small text-medium-emphasis mt-2">
      {{ t('imports.portal.files.hint') }}
    </p>
    <!-- Read out when the import ends: the report shows it on screen. -->
    <p role="status" class="status d-sr-only">{{ status }}</p>
  </div>
</template>
