<script setup lang="ts">
import { mdiCloseCircle, mdiDatabaseImport, mdiFileUploadOutline, mdiOpenInNew } from '@mdi/js'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { FileImport } from '@/entities/vehicle-import'
import { isApiError } from '@/shared/api'
import { useFormat } from '@/shared/lib'
import { ErrorSummary } from '@/shared/ui'
import { useImportAAOSLogging } from '../composables/useImportJob'

// AAOSLoggingImport imports the export of AAOS Logging, an Android Automotive app that
// records the car's trips and syncs them to the user's Google Drive: the ZIP Google
// Drive gives for the car's folder. Written at once, the report shows until the next
// import. The form waits behind a button, as the portal's.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const f = useFormat()
const { importExport, isPending, error, reset } = useImportAAOSLogging(() => props.vehicle)

const fileId = 'aaos-export'
const toggleId = 'aaos-import-toggle'
const opened = ref(false)
const exported = ref<File | null>(null)
const sent = ref(false)
const status = ref('')
// The latest report: choosing another file clears the mutation, not what it did.
const report = ref<FileImport | null>(null)

watch(exported, () => reset())

const fileError = computed(() => {
  if (sent.value && !exported.value) return t('imports.aaos.error.required')
  const e = error.value
  if (!e) return undefined
  if (isApiError(e, 'invalid_file')) return t('imports.aaos.error.file')
  if (isApiError(e, 'invalid_body')) return t('imports.aaos.error.tooLarge')
  if (isApiError(e, 'network')) return t('imports.aaos.error.network')
  return t('imports.aaos.error.internal')
})
const summary = computed(() => (fileError.value ? [{ field: fileId, text: fileError.value }] : []))
const summaryRef = ref<InstanceType<typeof ErrorSummary> | null>(null)

// Opened, the form focuses its field; closed, the focus goes back to its button.
async function openForm() {
  status.value = ''
  opened.value = true
  await nextTick()
  document.getElementById(fileId)?.focus()
}

async function closeForm() {
  exported.value = null
  sent.value = false
  opened.value = false
  await nextTick()
  reset()
  document.getElementById(toggleId)?.focus()
}

async function submit() {
  sent.value = true
  status.value = ''
  if (!exported.value) {
    await nextTick()
    summaryRef.value?.focus()
    return
  }
  try {
    report.value = await importExport(exported.value)
    exported.value = null
    sent.value = false
    opened.value = false
    await nextTick()
    document.getElementById(toggleId)?.focus()
    status.value = t('imports.aaos.done')
  } catch {
    await nextTick()
    summaryRef.value?.focus()
  }
}

const lines = computed(() => {
  const r = report.value
  if (!r) return []
  const out = [t('imports.aaos.report.imported', { n: r.imported }, r.imported)]
  if (r.skipped > 0)
    out.push(t('imports.aaos.report.skipped', { n: r.skipped, date: f.day(r.cutoff) }, r.skipped))
  if (r.invalid > 0) out.push(t('imports.aaos.report.invalid', { n: r.invalid }, r.invalid))
  return out
})
</script>

<template>
  <div class="aaos-import">
    <h4 class="text-title-medium mb-2">
      {{ t('imports.aaos.title') }}
      <v-chip size="small" variant="tonal" color="primary" class="ms-2 beta">
        {{ t('imports.aaos.beta') }}
      </v-chip>
    </h4>
    <p class="text-body-medium mb-2">{{ t('imports.aaos.intro') }}</p>
    <p class="text-body-medium mb-4">
      <a
        href="https://play.google.com/store/apps/details?id=com.aaoslogging"
        target="_blank"
        rel="noopener noreferrer"
        class="aaos-link text-primary"
      >
        {{ t('imports.aaos.link') }}<v-icon :icon="mdiOpenInNew" size="x-small" class="ms-1" />
        <span class="d-sr-only">{{ t('imports.aaos.newTab') }}</span>
      </a>
    </p>

    <v-alert
      v-if="lines.length"
      type="success"
      variant="tonal"
      :title="t('imports.aaos.done')"
      class="import-report mb-4"
    >
      <p v-for="line in lines" :key="line" class="text-body-medium">{{ line }}</p>
    </v-alert>

    <v-btn
      v-if="!opened"
      :id="toggleId"
      :prepend-icon="mdiFileUploadOutline"
      variant="tonal"
      class="toggle"
      @click="openForm"
    >
      {{ t('imports.aaos.open') }}
    </v-btn>
    <v-form v-else novalidate class="aaos-form" @submit.prevent="submit">
      <ErrorSummary
        v-if="summary.length"
        ref="summaryRef"
        :title="t('imports.aaos.error.summary')"
        :items="summary"
      />
      <v-file-input
        :id="fileId"
        v-model="exported"
        :label="t('imports.aaos.file.label')"
        :hint="t('imports.aaos.file.hint')"
        :error-messages="fileError"
        :aria-invalid="fileError ? 'true' : undefined"
        persistent-hint
        accept=".zip,application/zip"
        prepend-icon=""
        class="export file mb-4"
      >
        <template #clear="{ props: clear }">
          <v-icon
            v-bind="clear"
            :icon="mdiCloseCircle"
            role="button"
            :aria-label="t('imports.aaos.clear', { file: t('imports.aaos.file.label') })"
          />
        </template>
      </v-file-input>
      <v-btn
        type="submit"
        color="primary"
        :prepend-icon="mdiDatabaseImport"
        :loading="isPending"
        class="start"
      >
        {{ t('imports.aaos.start') }}
      </v-btn>
      <!-- Not while the file is sent: an import closed and opened again would take the
           earlier one's end for its own. -->
      <v-btn variant="text" :disabled="isPending" class="cancel ms-2" @click="closeForm">
        {{ t('imports.aaos.cancel') }}
      </v-btn>
    </v-form>
    <p role="status" class="status text-body-medium mt-2">{{ status }}</p>
  </div>
</template>

<style scoped>
.file {
  max-width: 32rem;
}
</style>
