<script setup lang="ts">
import { mdiCloseCircle, mdiDatabaseImport, mdiFileUploadOutline } from '@mdi/js'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { FileImport } from '@/entities/vehicle-import'
import { isApiError } from '@/shared/api'
import { useFormat } from '@/shared/lib'
import { ErrorSummary } from '@/shared/ui'
import { useAccountCurrency, useImportTeslaMate } from '../composables/useImportJob'

// TeslaMateImport imports the backup of a TeslaMate instance, the file its documentation
// tells every user to make: the drives with their tracks and the charges of the car of
// the vehicle's VIN. The browser compresses it before sending, then it is read as it
// comes and written at once; the report shows until the next import. TeslaMate keeps no
// currency: its costs are imported in the account's only when the user says so.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const f = useFormat()
const currency = useAccountCurrency()
const { importBackup, isPending, phase, error, reset } = useImportTeslaMate(() => props.vehicle)

const backupId = 'teslamate-backup'
const toggleId = 'teslamate-import-toggle'
const command = 'docker compose exec -T database pg_dump -U teslamate teslamate > teslamate.bck'
const opened = ref(false)
const backup = ref<File | null>(null)
const withCosts = ref(false)
const sent = ref(false)
const status = ref('')
const report = ref<FileImport | null>(null)

watch(backup, () => reset())

const missing = computed(() => sent.value && !backup.value)
const serverError = computed(() => {
  const e = error.value
  if (!e) return null
  if (isApiError(e, 'invalid_file')) return t('imports.teslamate.error.file')
  if (isApiError(e, 'no_matching_car')) return t('imports.teslamate.error.car')
  if (isApiError(e, 'invalid_body')) return t('imports.teslamate.error.tooLarge')
  if (isApiError(e, 'import_active')) return t('imports.teslamate.error.active')
  if (isApiError(e, 'source_unavailable')) return t('imports.teslamate.error.busy')
  if (isApiError(e, 'network')) return t('imports.teslamate.error.network')
  return t('imports.teslamate.error.internal')
})
const backupError = computed(() =>
  missing.value ? t('imports.teslamate.error.required') : (serverError.value ?? undefined),
)
const summary = computed(() =>
  backupError.value ? [{ field: backupId, text: backupError.value }] : [],
)
const summaryRef = ref<InstanceType<typeof ErrorSummary> | null>(null)

const progress = computed(() => {
  if (phase.value === 'compressing') return t('imports.teslamate.compressing')
  if (phase.value === 'sending') return t('imports.teslamate.sending')
  return ''
})

async function openForm() {
  status.value = ''
  opened.value = true
  await nextTick()
  document.getElementById(backupId)?.focus()
}

async function closeForm() {
  backup.value = null
  sent.value = false
  opened.value = false
  await nextTick()
  reset()
  document.getElementById(toggleId)?.focus()
}

async function submit() {
  sent.value = true
  status.value = ''
  if (!backup.value) {
    await nextTick()
    summaryRef.value?.focus()
    return
  }
  try {
    report.value = await importBackup({
      backup: backup.value,
      currency: withCosts.value && currency.value ? currency.value : undefined,
    })
    backup.value = null
    sent.value = false
    opened.value = false
    await nextTick()
    document.getElementById(toggleId)?.focus()
    status.value = t('imports.teslamate.done')
  } catch {
    await nextTick()
    summaryRef.value?.focus()
  }
}

const lines = computed(() => {
  const r = report.value
  if (!r) return []
  const out = [t('imports.teslamate.report.imported', { n: r.imported }, r.imported)]
  if (r.skipped > 0)
    out.push(
      t('imports.teslamate.report.skipped', { n: r.skipped, date: f.day(r.cutoff) }, r.skipped),
    )
  if (r.invalid > 0) out.push(t('imports.teslamate.report.invalid', { n: r.invalid }, r.invalid))
  return out
})
</script>

<template>
  <div class="teslamate-import">
    <h4 class="text-title-medium mb-2">
      {{ t('imports.teslamate.title') }}
      <v-chip size="small" variant="tonal" color="primary" class="ms-2 beta">
        {{ t('imports.teslamate.beta') }}
      </v-chip>
    </h4>
    <p class="text-body-medium mb-2">{{ t('imports.teslamate.intro') }}</p>
    <p class="text-body-medium mb-2">{{ t('imports.teslamate.how') }}</p>
    <pre class="command text-body-small mb-4"><code>{{ command }}</code></pre>

    <v-alert
      v-if="lines.length"
      type="success"
      variant="tonal"
      :title="t('imports.teslamate.done')"
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
      {{ t('imports.teslamate.open') }}
    </v-btn>
    <v-form v-else novalidate class="teslamate-form" @submit.prevent="submit">
      <ErrorSummary
        v-if="summary.length"
        ref="summaryRef"
        :title="t('imports.teslamate.error.summary')"
        :items="summary"
      />
      <v-file-input
        :id="backupId"
        v-model="backup"
        :label="t('imports.teslamate.backup.label')"
        :hint="t('imports.teslamate.backup.hint')"
        :error-messages="backupError"
        :aria-invalid="backupError ? 'true' : undefined"
        persistent-hint
        prepend-icon=""
        class="backup file mb-4"
      >
        <template #clear="{ props: clear }">
          <v-icon
            v-bind="clear"
            :icon="mdiCloseCircle"
            role="button"
            :aria-label="t('imports.teslamate.clear')"
          />
        </template>
      </v-file-input>
      <v-checkbox
        v-if="currency"
        v-model="withCosts"
        :label="t('imports.teslamate.costs', { currency: f.currencyName(currency) })"
        hide-details
        class="costs mb-4"
      />
      <p v-else class="no-currency text-body-medium text-medium-emphasis mb-4">
        {{ t('imports.teslamate.noCurrency') }}
      </p>
      <v-btn
        type="submit"
        color="primary"
        :prepend-icon="mdiDatabaseImport"
        :loading="isPending"
        class="start"
      >
        {{ t('imports.teslamate.start') }}
      </v-btn>
      <v-btn variant="text" :disabled="isPending" class="cancel ms-2" @click="closeForm">
        {{ t('imports.teslamate.cancel') }}
      </v-btn>
    </v-form>
    <p role="status" class="status text-body-medium mt-2">{{ progress || status }}</p>
  </div>
</template>

<style scoped>
.file {
  max-width: 32rem;
}
.command {
  white-space: pre-wrap;
  word-break: break-all;
  padding: 8px 12px;
  border-radius: var(--v-control-radius);
  background: rgb(var(--v-theme-surface-variant));
}
</style>
