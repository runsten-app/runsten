<script setup lang="ts">
import { mdiContentSave, mdiDelete, mdiKeyVariant, mdiOpenInNew } from '@mdi/js'
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ConnectionSettings } from '@/entities/connection'
import { isApiError } from '@/shared/api'
import { useFormat, useNow } from '@/shared/lib'
import { ErrorSummary, FactItem } from '@/shared/ui'
import { useDeletePolestarCredential } from '../composables/useDeletePolestarCredential'
import { useSetPolestarCredential } from '../composables/useSetPolestarCredential'
import { expiry } from '../model/expiry'
import {
  fieldId,
  fields,
  validate,
  type Draft,
  type Field,
  type Issue,
  type Rule,
} from '../model/validate'

// PolestarForm gives the account's credential of Polestar's Data Portal, which the owner
// creates there (the EU Data Act's machine-to-machine access): a client ID, its secret and
// the Account ID. The steps lead to the portal and to the guide. The credential is never
// shown again: the last four characters of its client ID, when it was given and when it
// expires (90 days), with a reminder two weeks before. Without one, the form is open;
// with one, it is replaced behind a button.
const props = defineProps<{ connection: ConnectionSettings }>()
const { t } = useI18n()
const f = useFormat()
const now = useNow()
const { setCredential, isPending, error, reset } = useSetPolestarCredential()

const credential = computed(() => props.connection.polestar)
const state = computed(() => {
  const c = credential.value
  if (!c) return null
  return c.refused_at ? 'refused' : expiry(c.expires_at, now.value)
})
const opened = ref(false)
const open = computed(() => opened.value || !credential.value)
const empty = (): Draft => ({ client_id: '', client_secret: '', account_id: '' })
const draft = reactive<Draft>(empty())
const sent = ref(false)
const status = ref('')

watch(draft, () => {
  status.value = ''
  reset()
})

const checked = computed(() => validate(draft))
const issues = computed(() => (sent.value ? checked.value.issues : []))
const labels = computed<Record<Field, string>>(() => ({
  client_id: t('polestar.field.clientId'),
  client_secret: t('polestar.field.clientSecret'),
  account_id: t('polestar.field.accountId'),
}))
const messages: Record<Rule, (field: string, p: Record<string, number>) => string> = {
  required: (field) => t('polestar.error.required', { field }),
  spaces: (field) => t('polestar.error.spaces', { field }),
  characters: (field) => t('polestar.error.characters', { field }),
  tooShort: (field, p) => t('polestar.error.tooShort', { field, min: p.min ?? 0 }),
  tooLong: (field, p) => t('polestar.error.tooLong', { field, max: p.max ?? 0 }),
}
const message = (i: Issue) => messages[i.rule](labels.value[i.field], i.params ?? {})
const refused = computed(() => !!error.value && isApiError(error.value, 'credential_refused'))
function fieldError(field: Field): string | undefined {
  const i = issues.value.find((i) => i.field === field)
  if (i) return message(i)
  // The Data Portal tells the credential refused, not which value: under the first.
  return refused.value && field === 'client_id' ? t('polestar.error.refused') : undefined
}
const serverError = computed(() => {
  const e = error.value
  if (!e) return null
  if (isApiError(e, 'credential_refused'))
    return { field: fieldId('client_id'), text: t('polestar.error.refused') }
  if (isApiError(e, 'invalid_body'))
    return { field: fieldId('client_id'), text: t('polestar.error.invalidBody') }
  if (isApiError(e, 'no_vehicle')) return { text: t('polestar.error.noVehicle') }
  if (isApiError(e, 'too_many_vehicles')) return { text: t('polestar.error.tooManyVehicles') }
  if (isApiError(e, 'not_admitted')) return { text: t('polestar.error.notAdmitted') }
  if (isApiError(e, 'source_unavailable')) return { text: t('polestar.error.portalUnavailable') }
  if (isApiError(e, 'unavailable') || isApiError(e, 'network'))
    return { text: t('polestar.error.unavailable') }
  return { text: t('polestar.error.internal') }
})
const summary = computed(() => [
  ...issues.value.map((i) => ({ field: fieldId(i.field), text: message(i) })),
  ...(serverError.value ? [serverError.value] : []),
])
const summaryRef = ref<InstanceType<typeof ErrorSummary> | null>(null)
async function focusSummary() {
  await nextTick()
  summaryRef.value?.focus()
}

const toggleId = 'polestar-toggle'
// Opened on request, the form focuses its first field once shown: no autofocus
// (Vuetify's takes the focus back 50 ms later).
async function openForm() {
  Object.assign(draft, empty())
  sent.value = false
  opened.value = true
  await nextTick()
  reset()
  status.value = ''
  document.getElementById(fieldId('client_id'))?.focus()
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
  try {
    await setCredential(body)
    await close(t('polestar.saved'))
  } catch {
    await focusSummary()
  }
}

const {
  deleteCredential,
  isPending: deletePending,
  error: deleteError,
  reset: resetDelete,
} = useDeletePolestarCredential()
const confirming = ref(false)
const deletedNow = ref(false)
const deleteId = 'polestar-delete'

function askDelete() {
  status.value = ''
  resetDelete()
  confirming.value = true
}

async function confirmDelete() {
  try {
    await deleteCredential()
    // The form opens at once, for the next credential: the connection is read again later.
    opened.value = true
    deletedNow.value = true
    confirming.value = false
  } catch {
    // Shown from the error, in the dialog.
  }
}

// Once the dialog closes, the focus goes back to its button; once the credential is
// deleted, that button is gone, and the form open: to its first field.
watch(confirming, async (v) => {
  if (v) return
  await nextTick()
  if (!deletedNow.value) {
    document.getElementById(deleteId)?.focus()
    return
  }
  deletedNow.value = false
  document.getElementById(fieldId('client_id'))?.focus()
  status.value = t('polestar.deleted')
})

// Polestar's Data Portal, where the owner creates the credential, and Runsten's guide:
// their names are the same in every language.
const portal = new URL('https://data-portal.polestar.com/')
const guide = new URL('https://runsten.app/docs/guide/polestar')
const autocomplete: Record<Field, string> = {
  client_id: 'off',
  client_secret: 'new-password',
  account_id: 'off',
}
</script>

<template>
  <div class="polestar-form">
    <p class="text-body-medium mb-1">
      {{ t('polestar.intro') }}
    </p>
    <p class="text-body-medium mb-3">
      {{ t('polestar.beta') }}
      <a :href="guide.href" target="_blank" rel="noreferrer" class="guide">
        {{ t('polestar.guide') }}<v-icon :icon="mdiOpenInNew" size="x-small" class="ms-1" />
        <span class="d-sr-only">{{ t('polestar.steps.newTab') }}</span>
      </a>
    </p>

    <v-alert
      v-if="credential && state === 'refused' && credential.refused_at"
      type="error"
      variant="tonal"
      :title="t('polestar.refused.title')"
      class="refused mb-4"
    >
      {{ t('polestar.refused.body', { time: f.dateTime(credential.refused_at, now) }) }}
    </v-alert>
    <v-alert
      v-else-if="credential && (state === 'soon' || state === 'past')"
      type="warning"
      variant="tonal"
      :title="t('polestar.renew.title')"
      class="renew mb-4"
    >
      {{
        state === 'soon'
          ? t('polestar.renew.soon', { date: f.day(credential.expires_at) })
          : t('polestar.renew.past', { date: f.day(credential.expires_at) })
      }}
    </v-alert>

    <dl v-if="credential" class="facts mb-4">
      <FactItem
        :label="t('polestar.current')"
        :value="t('polestar.last4', { last4: credential.last4 })"
      />
      <FactItem :label="t('polestar.setAt')" :value="f.dateTime(credential.set_at, now)" />
      <FactItem :label="t('polestar.expiresAt')" :value="f.day(credential.expires_at)" />
    </dl>

    <div v-if="!open || credential" class="d-flex flex-wrap ga-2 mb-4">
      <v-btn
        v-if="!open"
        :id="toggleId"
        :prepend-icon="mdiKeyVariant"
        :color="state === 'valid' ? undefined : 'primary'"
        variant="tonal"
        class="toggle"
        @click="openForm"
      >
        {{ t('polestar.replace') }}
      </v-btn>
      <v-btn
        v-if="credential"
        :id="deleteId"
        :prepend-icon="mdiDelete"
        variant="text"
        color="error"
        class="delete-credential"
        @click="askDelete"
      >
        {{ t('polestar.delete.button') }}
      </v-btn>
    </div>

    <v-dialog v-model="confirming" max-width="28rem" aria-labelledby="delete-polestar-title">
      <v-card>
        <v-card-title id="delete-polestar-title" tag="h2" class="text-title-large text-wrap">
          {{ t('polestar.delete.title') }}
        </v-card-title>
        <v-card-text>
          <p class="text-body-medium">
            {{ t('polestar.delete.text') }}
          </p>
          <v-alert v-if="deleteError" type="error" variant="tonal" class="mt-4">
            {{ t('polestar.delete.error') }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="confirming = false">{{
            t('polestar.delete.cancel')
          }}</v-btn>
          <v-btn
            color="error"
            variant="flat"
            :loading="deletePending"
            class="confirm-delete"
            @click="confirmDelete"
          >
            {{ t('polestar.delete.confirm') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-form
      v-if="open"
      id="polestar-form"
      novalidate
      class="credential-form"
      @submit.prevent="submit"
    >
      <ErrorSummary
        v-if="summary.length"
        ref="summaryRef"
        :title="t('polestar.error.summary')"
        :items="summary"
      />
      <h3 class="text-title-medium mb-2">{{ t('polestar.steps.title') }}</h3>
      <ol class="steps text-body-medium mb-4">
        <li>
          {{ t('polestar.steps.signIn') }}
          <a :href="portal.href" target="_blank" rel="noreferrer" class="portal">
            {{ portal.host }}<v-icon :icon="mdiOpenInNew" size="x-small" class="ms-1" />
            <span class="d-sr-only">{{ t('polestar.steps.newTab') }}</span>
          </a>
        </li>
        <li>{{ t('polestar.steps.create') }}</li>
        <li>{{ t('polestar.steps.domains') }}</li>
        <li>{{ t('polestar.steps.share') }}</li>
        <li>{{ t('polestar.steps.copy') }}</li>
      </ol>
      <v-text-field
        v-for="field in fields"
        :id="fieldId(field)"
        :key="field"
        v-model="draft[field]"
        :label="labels[field]"
        :type="field === 'client_secret' ? 'password' : 'text'"
        :error-messages="fieldError(field)"
        :aria-invalid="fieldError(field) ? 'true' : undefined"
        :autocomplete="autocomplete[field]"
        spellcheck="false"
        autocapitalize="none"
        class="value mb-2"
        :class="field"
      />
      <p class="text-body-small text-medium-emphasis mb-3">{{ t('polestar.help') }}</p>
      <div class="d-flex flex-wrap ga-3">
        <v-btn
          type="submit"
          color="primary"
          :prepend-icon="mdiContentSave"
          :loading="isPending"
          class="save"
        >
          {{ t('polestar.save') }}
        </v-btn>
        <v-btn v-if="opened" variant="text" class="cancel" @click="close()">
          {{ t('polestar.cancel') }}
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
