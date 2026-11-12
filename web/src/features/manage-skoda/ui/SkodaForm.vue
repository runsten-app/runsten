<script setup lang="ts">
import { mdiContentSave, mdiDelete, mdiKeyVariant, mdiOpenInNew } from '@mdi/js'
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ConnectionSettings, SkodaKey } from '@/entities/connection'
import { isApiError } from '@/shared/api'
import { useFormat, useNow } from '@/shared/lib'
import { ErrorSummary, FactItem } from '@/shared/ui'
import { useDeleteSkodaCredential } from '../composables/useDeleteSkodaCredential'
import { useSetSkodaCredential } from '../composables/useSetSkodaCredential'
import { keyState } from '../model/expiry'
import { fieldId, validate, type Draft, type Field, type Issue, type Rule } from '../model/validate'

// SkodaForm gives the account's MyŠkoda key, which the owner creates in the MyŠkoda app
// for the cars they tick, with the VIN of each: the API lists no vehicle. The steps lead
// to the app's page of the keys and to the guide. The key is never shown again: its last
// four characters, when it was given, and when it expires, as the API tells — the form
// warns two weeks before, and once it expired or was refused. Without one, the form is
// open; with one, it is replaced behind a button, the VINs of the account's Škodas
// filled in.
// vins: the VINs of the account's Škodas. off: the source is turned off for the account
// (the flag): a key given before stays told and can be removed; none is taken.
const props = defineProps<{ connection: ConnectionSettings; vins?: string[]; off?: boolean }>()
const { t } = useI18n()
const f = useFormat()
const now = useNow()
const { setCredential, error, reset } = useSetSkodaCredential()

const credential = computed((): SkodaKey | null => props.connection.skoda ?? null)
const state = computed(() => (credential.value ? keyState(credential.value, now.value) : null))
const opened = ref(false)
const open = computed(() => !props.off && (opened.value || !credential.value))
const empty = (): Draft => ({ api_key: '', vins: (props.vins ?? []).join('\n') })
const draft = reactive<Draft>(empty())
const sent = ref(false)
const status = ref('')
// saving holds from the request to its answer: the mutation's own state is cleared by
// reset(), which an edit of a field calls while the request still runs.
const saving = ref(false)

watch(draft, () => {
  status.value = ''
  if (!saving.value) reset()
})
// The account's Škodas, once read, fill an empty list in.
watch(
  () => props.vins,
  (vins) => {
    if (!draft.vins && vins?.length) draft.vins = vins.join('\n')
  },
)

const checked = computed(() => validate(draft))
const issues = computed(() => (sent.value ? checked.value.issues : []))
const labels = computed<Record<Field, string>>(() => ({
  api_key: t('skoda.field.apiKey'),
  vins: t('skoda.field.vins'),
}))
type Params = Record<string, string | number>
const messages: Record<Rule, (p: Params) => string> = {
  required: () => t('skoda.error.required'),
  spaces: () => t('skoda.error.spaces'),
  characters: () => t('skoda.error.characters'),
  tooShort: (p) => t('skoda.error.tooShort', { min: p.min ?? 0 }),
  tooLong: (p) => t('skoda.error.tooLong', { max: p.max ?? 0 }),
  vinRequired: () => t('skoda.error.vinRequired'),
  vinInvalid: (p) => t('skoda.error.vinInvalid', { vin: p.vin ?? '' }),
  vinTwice: (p) => t('skoda.error.vinTwice', { vin: p.vin ?? '' }),
  tooManyVins: (p) => t('skoda.error.tooManyVins', { max: p.max ?? 0 }),
}
const message = (i: Issue) => messages[i.rule](i.params ?? {})
// vin_not_covered names the VIN in its message: the API's, in English, read for the VIN.
const uncovered = computed(() => {
  const e = error.value
  if (!isApiError(e, 'vin_not_covered')) return null
  return /[A-HJ-NPR-Z0-9]{17}/.exec(e.message)?.[0] ?? ''
})
const serverError = computed((): { field?: string; text: string } | null => {
  const e = error.value
  if (!e) return null
  if (isApiError(e, 'credential_refused'))
    return { field: fieldId('api_key'), text: t('skoda.error.refused') }
  if (isApiError(e, 'key_expired'))
    return { field: fieldId('api_key'), text: t('skoda.error.expired') }
  if (isApiError(e, 'invalid_body'))
    return { field: fieldId('vins'), text: t('skoda.error.invalidBody') }
  if (uncovered.value !== null)
    return { field: fieldId('vins'), text: t('skoda.error.notCovered', { vin: uncovered.value }) }
  if (isApiError(e, 'too_many_vehicles')) return { text: t('skoda.error.tooManyVehicles') }
  if (isApiError(e, 'not_admitted')) return { text: t('skoda.error.notAdmitted') }
  if (isApiError(e, 'source_unavailable')) return { text: t('skoda.error.sourceUnavailable') }
  if (isApiError(e, 'feature_unavailable')) return { text: t('skoda.error.featureUnavailable') }
  if (isApiError(e, 'unavailable') || isApiError(e, 'network'))
    return { text: t('skoda.error.unavailable') }
  return { text: t('skoda.error.internal') }
})
function fieldError(field: Field): string | undefined {
  const i = issues.value.find((i) => i.field === field)
  if (i) return message(i)
  const s = serverError.value
  return s?.field === fieldId(field) ? s.text : undefined
}
const summary = computed(() => [
  ...issues.value.map((i) => ({ field: fieldId(i.field), text: message(i) })),
  ...(serverError.value ? [serverError.value] : []),
])
const summaryRef = ref<InstanceType<typeof ErrorSummary> | null>(null)
async function focusSummary() {
  await nextTick()
  summaryRef.value?.focus()
}

const toggleId = 'skoda-toggle'
// Opened on request, the form focuses its first field once shown: no autofocus
// (Vuetify's takes the focus back 50 ms later).
async function openForm() {
  Object.assign(draft, empty())
  sent.value = false
  opened.value = true
  await nextTick()
  // A save still running keeps its state: its error is yet to be told.
  if (!saving.value) reset()
  status.value = ''
  document.getElementById(fieldId('api_key'))?.focus()
}

async function close(said = '') {
  opened.value = false
  Object.assign(draft, empty())
  sent.value = false
  await nextTick()
  document.getElementById(toggleId)?.focus()
  status.value = said
}

async function submit() {
  sent.value = true
  const body = checked.value.body
  if (!body) return focusSummary()
  if (saving.value) return
  saving.value = true
  try {
    await setCredential(body)
    await close(t('skoda.saved'))
  } catch {
    await focusSummary()
  } finally {
    saving.value = false
  }
}

const {
  deleteCredential,
  isPending: deletePending,
  error: deleteError,
  reset: resetDelete,
} = useDeleteSkodaCredential()
const confirming = ref(false)
const deletedNow = ref(false)
const deleteId = 'skoda-delete'

function askDelete() {
  status.value = ''
  resetDelete()
  confirming.value = true
}

async function confirmDelete() {
  try {
    await deleteCredential()
    // The form opens at once, for the next key: the connection is read again later.
    opened.value = true
    deletedNow.value = true
    confirming.value = false
  } catch {
    // Shown from the error, in the dialog.
  }
}

// Once the dialog closes, the focus goes back to its button; once the key is deleted,
// that button is gone, and the form open: to its first field.
watch(confirming, async (v) => {
  if (v) return
  await nextTick()
  if (!deletedNow.value) {
    document.getElementById(deleteId)?.focus()
    return
  }
  deletedNow.value = false
  document.getElementById(fieldId('api_key'))?.focus()
  status.value = t('skoda.deleted')
})

// Škoda's page of the keys, which opens them in the MyŠkoda app on a phone, and Runsten's
// guide: their names are the same in every language.
const keys = new URL('https://go.skoda.eu/api-keys')
const guide = new URL('https://runsten.app/docs/guide/skoda')
</script>

<template>
  <div class="skoda-form">
    <p class="text-body-medium mb-1">{{ t('skoda.intro') }}</p>
    <p class="text-body-medium mb-3">
      <a :href="guide.href" target="_blank" rel="noreferrer" class="guide">
        {{ t('skoda.guide') }}<v-icon :icon="mdiOpenInNew" size="x-small" class="ms-1" />
        <span class="d-sr-only">{{ t('skoda.steps.newTab') }}</span>
      </a>
    </p>

    <v-alert
      v-if="credential && state === 'refused' && credential.refused_at"
      type="error"
      variant="tonal"
      :title="t('skoda.refused.title')"
      class="refused mb-4"
    >
      {{ t('skoda.refused.body', { time: f.dateTime(credential.refused_at, now) }) }}
    </v-alert>
    <v-alert
      v-else-if="credential && state === 'expired' && credential.expires_at"
      type="error"
      variant="tonal"
      :title="t('skoda.expired.title')"
      class="expired mb-4"
    >
      {{ t('skoda.expired.body', { date: f.day(credential.expires_at) }) }}
    </v-alert>
    <v-alert
      v-else-if="credential && state === 'soon' && credential.expires_at"
      type="warning"
      variant="tonal"
      :title="t('skoda.renew.title')"
      class="renew mb-4"
    >
      {{ t('skoda.renew.body', { date: f.day(credential.expires_at) }) }}
    </v-alert>

    <dl v-if="credential" class="facts mb-4">
      <FactItem
        :label="t('skoda.current')"
        :value="t('skoda.last4', { last4: credential.last4 })"
      />
      <FactItem :label="t('skoda.setAt')" :value="f.dateTime(credential.set_at, now)" />
      <FactItem
        :label="t('skoda.expiresAt')"
        :value="credential.expires_at ? f.day(credential.expires_at) : t('skoda.expiresUnknown')"
      />
    </dl>

    <div v-if="!open || credential" class="d-flex flex-wrap ga-2 mb-4">
      <v-btn
        v-if="!open && !off"
        :id="toggleId"
        :prepend-icon="mdiKeyVariant"
        variant="tonal"
        class="toggle"
        @click="openForm"
      >
        {{ t('skoda.replace') }}
      </v-btn>
      <v-btn
        v-if="credential"
        :id="deleteId"
        :prepend-icon="mdiDelete"
        variant="text"
        color="error"
        class="delete-credential"
        :disabled="saving"
        @click="askDelete"
      >
        {{ t('skoda.delete.button') }}
      </v-btn>
    </div>

    <v-dialog v-model="confirming" max-width="28rem" aria-labelledby="delete-skoda-title">
      <v-card>
        <v-card-title id="delete-skoda-title" tag="h2" class="text-title-large text-wrap">
          {{ t('skoda.delete.title') }}
        </v-card-title>
        <v-card-text>
          <p class="text-body-medium">{{ t('skoda.delete.text') }}</p>
          <v-alert v-if="deleteError" type="error" variant="tonal" class="mt-4">
            {{ t('skoda.delete.error') }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="confirming = false">{{ t('skoda.delete.cancel') }}</v-btn>
          <v-btn
            color="error"
            variant="flat"
            :loading="deletePending"
            class="confirm-delete"
            @click="confirmDelete"
          >
            {{ t('skoda.delete.confirm') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-form v-if="open" id="skoda-form" novalidate class="credential-form" @submit.prevent="submit">
      <ErrorSummary
        v-if="summary.length"
        ref="summaryRef"
        :title="t('skoda.error.summary')"
        :items="summary"
      />
      <h3 class="text-title-medium mb-2">{{ t('skoda.steps.title') }}</h3>
      <ol class="steps text-body-medium mb-4">
        <li>
          {{ t('skoda.steps.open') }}
          <a :href="keys.href" target="_blank" rel="noreferrer" class="keys">
            {{ keys.host + keys.pathname
            }}<v-icon :icon="mdiOpenInNew" size="x-small" class="ms-1" />
            <span class="d-sr-only">{{ t('skoda.steps.newTab') }}</span>
          </a>
        </li>
        <li>{{ t('skoda.steps.create') }}</li>
        <li>{{ t('skoda.steps.copy') }}</li>
      </ol>
      <v-text-field
        :id="fieldId('api_key')"
        v-model="draft.api_key"
        :label="labels.api_key"
        type="password"
        :error-messages="fieldError('api_key')"
        :aria-invalid="fieldError('api_key') ? 'true' : undefined"
        autocomplete="new-password"
        spellcheck="false"
        autocapitalize="none"
        class="value api_key mb-2"
      />
      <v-textarea
        :id="fieldId('vins')"
        v-model="draft.vins"
        :label="labels.vins"
        :hint="t('skoda.vinsHint')"
        persistent-hint
        rows="2"
        auto-grow
        :error-messages="fieldError('vins')"
        :aria-invalid="fieldError('vins') ? 'true' : undefined"
        autocomplete="off"
        spellcheck="false"
        autocapitalize="characters"
        class="value vins mb-2"
      />
      <p class="text-body-small text-medium-emphasis mb-3">{{ t('skoda.help') }}</p>
      <div class="d-flex flex-wrap ga-3">
        <v-btn
          type="submit"
          color="primary"
          :prepend-icon="mdiContentSave"
          :loading="saving"
          class="save"
        >
          {{ t('skoda.save') }}
        </v-btn>
        <v-btn v-if="opened && credential" variant="text" class="cancel" @click="close()">
          {{ t('skoda.cancel') }}
        </v-btn>
      </div>
    </v-form>
    <p role="status" class="status text-body-medium mt-2">{{ status }}</p>
  </div>
</template>

<style scoped>
.facts {
  display: grid;
  gap: 1rem;
  grid-template-columns: repeat(auto-fill, minmax(14rem, 1fr));
}
.steps {
  padding-inline-start: 1.5rem;
}
.steps li + li {
  margin-top: 0.25rem;
}
.value {
  max-width: 32rem;
}
</style>
