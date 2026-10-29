<script setup lang="ts">
import { mdiCarSearch, mdiDatabaseImport, mdiRefresh } from '@mdi/js'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { isActive, type AximoteVehicle, type ImportJob } from '@/entities/vehicle-import'
import { isApiError } from '@/shared/api'
import { useFormat, useNow } from '@/shared/lib'
import { ErrorSummary } from '@/shared/ui'
import { useAximoteVehicles, useImportJob, useStartImport } from '../composables/useImportJob'
import { fields, validateImport, validateToken, type Issue, type Rule } from '../model/validate'

// AximoteImport imports the vehicle's past trips and charges from Aximote, with a
// personal access token of its Pro offer: the token lists the account's vehicles, the
// one with this vehicle's VIN chosen at once, else by the user, never guessed. The import
// runs in the background: its progress is read again while it runs, its report stays
// until the next one. The form is open before the first import, behind a button after
// it. The token is never shown again, and leaves the page once sent.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const f = useFormat()
const now = useNow()
const { job } = useImportJob(() => props.vehicle)
const {
  listVehicles,
  vehicles,
  isPending: listing,
  error: listError,
  reset: resetList,
} = useAximoteVehicles(() => props.vehicle)
const {
  startImport,
  isPending: starting,
  error: startError,
  reset: resetStart,
} = useStartImport(() => props.vehicle)

const active = computed(() => isActive(job.value))
const opened = ref(false)
const formShown = computed(() => !active.value && (opened.value || !job.value))
const toggleId = 'aximote-import-toggle'

// Opened on request, the form focuses its field once shown: no autofocus (Vuetify's
// takes the focus back 50 ms later).
async function openForm() {
  status.value = ''
  opened.value = true
  await nextTick()
  document.getElementById(fields.token)?.focus()
}

async function closeForm() {
  draft.value = ''
  sent.value = false
  opened.value = false
  await nextTick()
  resetList()
  document.getElementById(toggleId)?.focus()
}
const draft = ref('')
const chosen = ref<string | null>(null)
const sent = ref(false)
const status = ref('')

// Another token lists other vehicles: those listed go.
watch(draft, () => {
  if (vehicles.value) chosen.value = null
  resetList()
  resetStart()
})
watch(vehicles, (list) => {
  chosen.value = list?.find((v) => v.vin_matches)?.id ?? null
})
watch(
  () => job.value?.status,
  (s, before) => {
    if ((before === 'pending' || before === 'running') && s === 'done')
      status.value = t('imports.aximote.report.doneTitle')
    if ((before === 'pending' || before === 'running') && s === 'failed')
      status.value = t('imports.aximote.report.failedTitle')
  },
)

const checked = computed(() =>
  vehicles.value ? validateImport(draft.value, chosen.value) : validateToken(draft.value),
)
const issues = computed(() => (sent.value ? checked.value.issues : []))
const messages: Record<Rule, () => string> = {
  required: () => t('imports.aximote.error.required'),
  format: () => t('imports.aximote.error.format'),
  vehicle: () => t('imports.aximote.error.vehicle'),
}
const issueOf = (field: string) => issues.value.find((i: Issue) => i.field === field)
const serverError = computed(() => {
  const e = listError.value ?? startError.value
  if (!e) return null
  if (isApiError(e, 'token_refused'))
    return { field: fields.token, text: t('imports.aximote.error.refused') }
  if (isApiError(e, 'invalid_body'))
    return { field: fields.token, text: t('imports.aximote.error.invalidBody') }
  if (isApiError(e, 'source_unavailable')) return { text: t('imports.aximote.error.unavailable') }
  if (isApiError(e, 'import_active')) return { text: t('imports.aximote.error.active') }
  if (isApiError(e, 'network')) return { text: t('imports.aximote.error.network') }
  return { text: t('imports.aximote.error.internal') }
})
const tokenError = computed(() => {
  const i = issueOf(fields.token)
  if (i) return messages[i.rule]()
  return serverError.value?.field === fields.token ? serverError.value.text : undefined
})
const vehicleError = computed(() => {
  const i = issueOf(fields.vehicle)
  return i ? messages[i.rule]() : undefined
})
const summary = computed(() => [
  ...issues.value.map((i) => ({ field: i.field, text: messages[i.rule]() })),
  ...(serverError.value ? [serverError.value] : []),
])
const summaryRef = ref<InstanceType<typeof ErrorSummary> | null>(null)
async function focusSummary() {
  await nextTick()
  summaryRef.value?.focus()
}

const matched = computed(() => vehicles.value?.some((v) => v.vin_matches) ?? false)
function label(v: AximoteVehicle) {
  const model = [v.make, v.model].filter(Boolean).join(' ')
  const name = [v.name, model].filter(Boolean).join(' · ') || v.id
  return v.year ? `${name} (${v.year})` : name
}

async function submit() {
  sent.value = true
  status.value = ''
  if (!vehicles.value) {
    const { token } = validateToken(draft.value)
    if (!token) return focusSummary()
    try {
      const list = await listVehicles(token)
      sent.value = false
      status.value = t('imports.aximote.listed', { n: list.length }, list.length)
    } catch {
      await focusSummary()
    }
    return
  }
  const { body } = validateImport(draft.value, chosen.value)
  if (!body) return focusSummary()
  try {
    await startImport(body)
    draft.value = ''
    opened.value = false
    await nextTick()
    resetList()
    sent.value = false
    status.value = t('imports.aximote.started')
  } catch {
    await focusSummary()
  }
}

const imported = (j: ImportJob) =>
  t('imports.aximote.report.imported', { n: j.imported }, j.imported)
const report = computed(() => {
  const j = job.value
  if (!j || active.value) return null
  const lines = [imported(j)]
  if (j.skipped > 0 && j.cutoff)
    lines.push(
      t('imports.aximote.report.skipped', { n: j.skipped, date: f.day(j.cutoff) }, j.skipped),
    )
  if (j.invalid > 0) lines.push(t('imports.aximote.report.invalid', { n: j.invalid }, j.invalid))
  return lines
})
const failure = computed(() => {
  switch (job.value?.error) {
    case 'token_refused':
      return t('imports.aximote.report.tokenRefused')
    case 'source_failed':
      return t('imports.aximote.report.sourceFailed')
    case 'internal':
      return t('imports.aximote.report.internal')
    default:
      return ''
  }
})
const waiting = computed(() => {
  const j = job.value
  return j?.status === 'pending' && new Date(j.not_before).getTime() > now.value
})
</script>

<template>
  <div class="aximote-import">
    <h4 class="text-title-medium mb-2">
      {{ t('imports.aximote.title') }}
      <v-chip size="small" variant="tonal" color="primary" class="ms-2 beta">
        {{ t('imports.aximote.beta') }}
      </v-chip>
    </h4>
    <p class="text-body-medium mb-4">{{ t('imports.aximote.intro') }}</p>

    <div v-if="job && active" class="import-progress mb-4">
      <v-progress-linear indeterminate :aria-label="t('imports.aximote.progress.label')" />
      <p class="text-body-medium mt-2 step">
        {{
          job.step === 'trips'
            ? t('imports.aximote.progress.trips')
            : t('imports.aximote.progress.charges')
        }}
        {{ imported(job) }}
      </p>
      <p v-if="waiting" class="text-body-medium text-medium-emphasis waiting">
        {{ t('imports.aximote.progress.waiting', { time: f.dateTime(job.not_before, now) }) }}
      </p>
    </div>
    <v-alert
      v-else-if="job && report"
      :type="job.status === 'done' ? 'success' : 'error'"
      variant="tonal"
      :title="
        job.status === 'done'
          ? t('imports.aximote.report.doneTitle')
          : t('imports.aximote.report.failedTitle')
      "
      class="import-report mb-4"
    >
      <p v-if="failure" class="text-body-medium failure">{{ failure }}</p>
      <p v-for="line in report" :key="line" class="text-body-medium">{{ line }}</p>
      <p class="text-body-medium text-medium-emphasis">
        {{
          t('imports.aximote.report.when', {
            time: f.dateTime(job.ended_at ?? job.created_at, now),
          })
        }}
      </p>
    </v-alert>

    <v-btn
      v-if="!active && !formShown"
      :id="toggleId"
      :prepend-icon="mdiRefresh"
      variant="tonal"
      class="toggle"
      @click="openForm"
    >
      {{ t('imports.aximote.again') }}
    </v-btn>
    <v-form v-if="formShown" novalidate class="import-form" @submit.prevent="submit">
      <ErrorSummary
        v-if="summary.length"
        ref="summaryRef"
        :title="t('imports.aximote.error.summary')"
        :items="summary"
      />
      <v-text-field
        :id="fields.token"
        v-model="draft"
        :label="t('imports.aximote.token.label')"
        :error-messages="tokenError"
        :aria-invalid="tokenError ? 'true' : undefined"
        :hint="t('imports.aximote.token.hint')"
        persistent-hint
        autocomplete="off"
        spellcheck="false"
        autocapitalize="none"
        class="token mb-4"
      />
      <fieldset
        v-if="vehicles"
        :id="fields.vehicle"
        tabindex="-1"
        class="vehicles mb-4"
        :aria-describedby="vehicleError ? 'aximote-vehicle-error' : undefined"
      >
        <legend class="text-title-small mb-2">{{ t('imports.aximote.vehicle.legend') }}</legend>
        <p v-if="!vehicles.length" class="text-body-medium none">
          {{ t('imports.aximote.vehicle.none') }}
        </p>
        <p v-else-if="!matched" class="text-body-medium mb-2 no-match">
          {{ t('imports.aximote.vehicle.noMatch') }}
        </p>
        <v-radio-group v-model="chosen" hide-details>
          <v-radio v-for="v in vehicles" :key="v.id" :value="v.id" class="vehicle">
            <template #label>
              <span>{{ label(v) }}</span>
              <v-chip v-if="v.vin_matches" size="small" color="success" class="ms-2 match">
                {{ t('imports.aximote.vehicle.matches') }}
              </v-chip>
            </template>
          </v-radio>
        </v-radio-group>
        <p
          v-if="vehicleError"
          id="aximote-vehicle-error"
          class="text-body-small text-error mt-1 vehicle-error"
        >
          {{ vehicleError }}
        </p>
      </fieldset>
      <v-btn
        v-if="!vehicles"
        type="submit"
        color="primary"
        variant="tonal"
        :prepend-icon="mdiCarSearch"
        :loading="listing"
        class="list"
      >
        {{ t('imports.aximote.list') }}
      </v-btn>
      <v-btn
        v-else
        type="submit"
        color="primary"
        :prepend-icon="mdiDatabaseImport"
        :loading="starting"
        class="start"
      >
        {{ t('imports.aximote.start') }}
      </v-btn>
      <v-btn v-if="opened" variant="text" class="cancel ms-2" @click="closeForm">
        {{ t('imports.aximote.cancel') }}
      </v-btn>
    </v-form>
    <p role="status" class="status text-body-medium mt-2">{{ status }}</p>
  </div>
</template>

<style scoped>
.token {
  max-width: 32rem;
}
.vehicles {
  border: 0;
  padding: 0;
  margin-inline: 0;
}
</style>
