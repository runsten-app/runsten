<script setup lang="ts">
import { mdiCloseCircle, mdiDatabaseImport, mdiFileUploadOutline, mdiOpenInNew } from '@mdi/js'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { FileImport } from '@/entities/vehicle-import'
import { isApiError } from '@/shared/api'
import { useFormat } from '@/shared/lib'
import { ErrorSummary } from '@/shared/ui'
import { useImportVolvoDataPortal } from '../composables/useImportJob'

// VolvoDataPortalImport imports the files an owner in the EU gets from the Volvo Data
// Portal under the Data Act: the trips, and the waypoints that make their tracks. They
// are written at once, and the report shows until the next import. The form waits behind
// a button: most visits come for the list, or for Aximote.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const f = useFormat()
const { importFiles, isPending, error, reset } = useImportVolvoDataPortal(() => props.vehicle)

const tripsId = 'portal-trips'
const toggleId = 'portal-import-toggle'
const opened = ref(false)
const trips = ref<File | null>(null)
const waypoints = ref<File | null>(null)
const sent = ref(false)
const status = ref('')
// The latest report: choosing other files clears the mutation, not what it did.
const report = ref<FileImport | null>(null)

watch([trips, waypoints], () => reset())

const missing = computed(() => sent.value && !trips.value)
const serverError = computed(() => {
  const e = error.value
  if (!e) return null
  if (isApiError(e, 'invalid_file')) return t('imports.portal.error.file')
  if (isApiError(e, 'invalid_body')) return t('imports.portal.error.tooLarge')
  if (isApiError(e, 'network')) return t('imports.portal.error.network')
  return t('imports.portal.error.internal')
})
const tripsError = computed(() =>
  missing.value ? t('imports.portal.error.required') : (serverError.value ?? undefined),
)
const summary = computed(() =>
  tripsError.value ? [{ field: tripsId, text: tripsError.value }] : [],
)
const summaryRef = ref<InstanceType<typeof ErrorSummary> | null>(null)

// Opened, the form focuses its first field; closed, the focus goes back to its button.
async function openForm() {
  status.value = ''
  opened.value = true
  await nextTick()
  document.getElementById(tripsId)?.focus()
}

async function closeForm() {
  trips.value = null
  waypoints.value = null
  sent.value = false
  opened.value = false
  await nextTick()
  reset()
  document.getElementById(toggleId)?.focus()
}

async function submit() {
  sent.value = true
  status.value = ''
  if (!trips.value) {
    await nextTick()
    summaryRef.value?.focus()
    return
  }
  try {
    report.value = await importFiles({
      trips: trips.value,
      waypoints: waypoints.value ?? undefined,
    })
    trips.value = null
    waypoints.value = null
    sent.value = false
    opened.value = false
    await nextTick()
    document.getElementById(toggleId)?.focus()
    status.value = t('imports.portal.done')
  } catch {
    await nextTick()
    summaryRef.value?.focus()
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

    <v-btn
      v-if="!opened"
      :id="toggleId"
      :prepend-icon="mdiFileUploadOutline"
      variant="tonal"
      class="toggle"
      @click="openForm"
    >
      {{ t('imports.portal.open') }}
    </v-btn>
    <v-form v-else novalidate class="portal-form" @submit.prevent="submit">
      <ErrorSummary
        v-if="summary.length"
        ref="summaryRef"
        :title="t('imports.portal.error.summary')"
        :items="summary"
      />
      <v-file-input
        :id="tripsId"
        v-model="trips"
        :label="t('imports.portal.trips.label')"
        :hint="t('imports.portal.trips.hint')"
        :error-messages="tripsError"
        :aria-invalid="tripsError ? 'true' : undefined"
        persistent-hint
        accept=".csv,text/csv"
        prepend-icon=""
        class="trips file mb-4"
      >
        <template #clear="{ props: clear }">
          <v-icon
            v-bind="clear"
            :icon="mdiCloseCircle"
            role="button"
            :aria-label="t('imports.portal.clear', { file: t('imports.portal.trips.label') })"
          />
        </template>
      </v-file-input>
      <v-file-input
        id="portal-waypoints"
        v-model="waypoints"
        :label="t('imports.portal.waypoints.label')"
        :hint="t('imports.portal.waypoints.hint')"
        persistent-hint
        accept=".csv,text/csv"
        prepend-icon=""
        class="waypoints file mb-4"
      >
        <template #clear="{ props: clear }">
          <v-icon
            v-bind="clear"
            :icon="mdiCloseCircle"
            role="button"
            :aria-label="t('imports.portal.clear', { file: t('imports.portal.waypoints.label') })"
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
        {{ t('imports.portal.start') }}
      </v-btn>
      <v-btn variant="text" class="cancel ms-2" @click="closeForm">
        {{ t('imports.portal.cancel') }}
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
