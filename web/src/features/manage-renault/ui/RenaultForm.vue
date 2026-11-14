<script setup lang="ts">
import { mdiDelete, mdiLogin, mdiOpenInNew, mdiSend } from '@mdi/js'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ConnectionSettings } from '@/entities/connection'
import { isApiError } from '@/shared/api'
import { useFormat, useNow } from '@/shared/lib'
import { ErrorSummary, FactItem } from '@/shared/ui'
import { useDeleteRenault } from '../composables/useDeleteRenault'
import { useRenaultCode } from '../composables/useRenaultCode'
import { useSignInRenault } from '../composables/useSignInRenault'
import {
  codeId,
  emailId,
  passwordId,
  validateCode,
  validateSignIn,
  type Issue,
} from '../model/validate'

// RenaultForm signs the owner in to My Renault (ADR 0050), the app that also serves Dacia
// (MyDacia) and Alpine (MyAlpine) with the same account: an unofficial API, which may stop
// when Renault changes its app. The email and password travel once and are never stored,
// only the session Renault returns. Some accounts get a one-time code by email: the form
// then asks for it, on a step of its own, for the ten minutes Runsten waits. Neither the
// password nor the session is shown again: the last four characters of the account's id,
// when, and whether Renault refused it since. Without a sign-in, the form is open; with
// one, it is replaced behind a button.
// off: the source is turned off for the account (the flag): a sign-in given before stays
// told and can be removed; none is taken.
const props = defineProps<{ connection: ConnectionSettings; off?: boolean }>()
const { t, locale } = useI18n()
const f = useFormat()
const now = useNow()
const signIn = useSignInRenault()
const code = useRenaultCode()

const current = computed(() => props.connection.renault ?? null)
const opened = ref(false)
const open = computed(() => !props.off && (opened.value || !current.value))
const email = ref('')
const password = ref('')
const codeDraft = ref('')
// The address Renault sent the one-time code to, masked: set while the code is awaited.
const sentTo = ref<string | null>(null)
// How many vehicles of the account are not electric, so not read: told once a sign-in
// connected, which the code's answer does not repeat.
const others = ref(0)
const sent = ref(false)
const status = ref('')
// saving holds from the request to its answer: the mutation's own state is cleared by
// reset(), which an edit of a field calls while the request still runs.
const saving = ref(false)
// The refusal to tell: the code's, which ends its step and is told on the sign-in's, else
// the sign-in's. Each request clears the other's.
const failure = computed(() => code.error.value ?? signIn.error.value)

watch([email, password, codeDraft], () => {
  status.value = ''
  if (saving.value) return
  signIn.reset()
  code.reset()
})

const issues = computed<Issue[]>(() => {
  if (!sent.value) return []
  return sentTo.value === null
    ? validateSignIn(email.value, password.value).issues
    : validateCode(codeDraft.value).issues
})
const message = (i: Issue) => {
  switch (i.rule) {
    case 'required':
      if (i.field === emailId) return t('renault.error.emailRequired')
      if (i.field === passwordId) return t('renault.error.passwordRequired')
      return t('renault.error.codeRequired')
    case 'email':
      return t('renault.error.email')
    case 'tooLong':
      return t('renault.error.tooLong', { max: i.params?.max ?? 0 })
    case 'codeFormat':
      return t('renault.error.codeFormat')
  }
}
const serverError = computed((): { field?: string; text: string } | null => {
  const e = failure.value
  if (!e) return null
  if (isApiError(e, 'credential_refused'))
    return { field: passwordId, text: t('renault.error.refused') }
  if (isApiError(e, 'wrong_code')) return { field: codeId, text: t('renault.error.wrongCode') }
  if (isApiError(e, 'authorization_expired'))
    return { field: emailId, text: t('renault.error.expired') }
  if (isApiError(e, 'invalid_body')) return { field: emailId, text: t('renault.error.invalidBody') }
  if (isApiError(e, 'no_vehicle')) return { text: t('renault.error.noVehicle') }
  if (isApiError(e, 'too_many_vehicles')) return { text: t('renault.error.tooManyVehicles') }
  if (isApiError(e, 'not_admitted')) return { text: t('renault.error.notAdmitted') }
  if (isApiError(e, 'feature_unavailable')) return { text: t('renault.error.featureUnavailable') }
  if (isApiError(e, 'rate_limited')) return { text: t('renault.error.rateLimited') }
  if (isApiError(e, 'source_unavailable')) return { text: t('renault.error.sourceUnavailable') }
  if (isApiError(e, 'unavailable') || isApiError(e, 'network'))
    return { text: t('renault.error.unavailable') }
  return { text: t('renault.error.internal') }
})
const fieldError = (field: string) => {
  const i = issues.value.find((x) => x.field === field)
  if (i) return message(i)
  return serverError.value?.field === field ? serverError.value.text : undefined
}
const summary = computed(() => [
  ...issues.value.map((i) => ({ field: i.field, text: message(i) })),
  ...(serverError.value ? [serverError.value] : []),
])
const summaryRef = ref<InstanceType<typeof ErrorSummary> | null>(null)
async function focusSummary() {
  await nextTick()
  summaryRef.value?.focus()
}

const toggleId = 'renault-toggle'

// The form back at its first step, empty: the email and password asked, no code awaited.
function resetSteps() {
  email.value = password.value = codeDraft.value = ''
  sentTo.value = null
  others.value = 0
  sent.value = false
  // A request still running keeps its state: its error is yet to be told.
  if (!saving.value) {
    signIn.reset()
    code.reset()
  }
}

async function openForm() {
  resetSteps()
  opened.value = true
  await nextTick()
  status.value = ''
  document.getElementById(emailId)?.focus()
}

async function close(said = '') {
  opened.value = false
  email.value = password.value = codeDraft.value = ''
  sentTo.value = null
  sent.value = false
  await nextTick()
  document.getElementById(toggleId)?.focus()
  status.value = said
}

// The code's step ends, its sign-in being over on Runsten's side (expired, or refused):
// the owner signs in again. The password is asked again: it was only kept in memory by the
// API, never here.
async function backToSignIn() {
  sentTo.value = null
  codeDraft.value = ''
  sent.value = false
  await focusSummary()
}

// Back, by the owner's choice: nothing to tell of the sign-in left.
function back() {
  code.reset()
  signIn.reset()
  void backToSignIn()
}

async function submitSignIn() {
  const { body } = validateSignIn(email.value, password.value)
  if (!body) return focusSummary()
  const language = locale.value.slice(0, 2)
  code.reset()
  try {
    const answer = await signIn.signIn({ ...body, language })
    password.value = ''
    if (answer.code_sent_to !== null) {
      sentTo.value = answer.code_sent_to
      sent.value = false
      await nextTick()
      document.getElementById(codeId)?.focus()
      return
    }
    others.value = answer.others
    await close(t('renault.saved'))
  } catch {
    await focusSummary()
  }
}

async function submitCode() {
  const { code: value } = validateCode(codeDraft.value)
  if (!value) return focusSummary()
  signIn.reset()
  try {
    const answer = await code.enterCode(value)
    others.value = answer.others
    await close(t('renault.saved'))
  } catch (e) {
    // A wrong code leaves the sign-in waiting; any other refusal ends it.
    if (isApiError(e, 'wrong_code')) return focusSummary()
    await backToSignIn()
  }
}

async function submit() {
  sent.value = true
  if (saving.value) return
  saving.value = true
  try {
    if (sentTo.value === null) await submitSignIn()
    else await submitCode()
  } finally {
    saving.value = false
  }
}

const {
  remove,
  isPending: deletePending,
  error: deleteError,
  reset: resetDelete,
} = useDeleteRenault()
const confirming = ref(false)
const deletedNow = ref(false)
const deleteId = 'renault-delete'

function askDelete() {
  status.value = ''
  resetDelete()
  confirming.value = true
}

async function confirmDelete() {
  try {
    await remove()
    // The form opens at once, at its first step, for the next sign-in: a code awaited
    // went with the sign-in. The connection is read again later.
    resetSteps()
    opened.value = true
    deletedNow.value = true
    confirming.value = false
  } catch {
    // Shown from the error, in the dialog.
  }
}

// Once the dialog closes, the focus goes back to its button; once the sign-in is removed,
// that button is gone, and the form open: to its first field.
watch(confirming, async (v) => {
  if (v) return
  await nextTick()
  if (!deletedNow.value) {
    document.getElementById(deleteId)?.focus()
    return
  }
  deletedNow.value = false
  document.getElementById(emailId)?.focus()
  status.value = t('renault.deleted')
})

const guide = new URL('https://runsten.app/docs/guide/renault')
</script>

<template>
  <div class="renault-form">
    <p class="text-body-medium mb-1">{{ t('renault.intro') }}</p>
    <p class="text-body-medium mb-3">
      <a :href="guide.href" target="_blank" rel="noreferrer" class="guide">
        {{ t('renault.guide') }}<v-icon :icon="mdiOpenInNew" size="x-small" class="ms-1" />
        <span class="d-sr-only">{{ t('renault.newTab') }}</span>
      </a>
    </p>

    <v-alert
      v-if="current?.refused_at"
      type="error"
      variant="tonal"
      :title="t('renault.refused.title')"
      class="refused mb-4"
    >
      {{ t('renault.refused.body', { time: f.dateTime(current.refused_at, now) }) }}
    </v-alert>
    <v-alert v-if="others > 0 && current && !open" type="info" variant="tonal" class="others mb-4">
      {{ t('renault.others', others) }}
    </v-alert>

    <dl v-if="current" class="facts mb-4">
      <FactItem
        :label="t('renault.current')"
        :value="t('renault.last4', { last4: current.last4 })"
      />
      <FactItem :label="t('renault.setAt')" :value="f.dateTime(current.set_at, now)" />
    </dl>

    <div v-if="!open || current" class="d-flex flex-wrap ga-2 mb-4">
      <v-btn
        v-if="!open && !off"
        :id="toggleId"
        :prepend-icon="mdiLogin"
        :color="current?.refused_at ? 'primary' : undefined"
        variant="tonal"
        class="toggle"
        @click="openForm"
      >
        {{ t('renault.reconnect') }}
      </v-btn>
      <v-btn
        v-if="current"
        :id="deleteId"
        :prepend-icon="mdiDelete"
        variant="text"
        color="error"
        class="delete-connection"
        :disabled="saving"
        @click="askDelete"
      >
        {{ t('renault.delete.button') }}
      </v-btn>
    </div>

    <v-dialog v-model="confirming" max-width="28rem" aria-labelledby="delete-renault-title">
      <v-card>
        <v-card-title id="delete-renault-title" tag="h2" class="text-title-large text-wrap">
          {{ t('renault.delete.title') }}
        </v-card-title>
        <v-card-text>
          <p class="text-body-medium">{{ t('renault.delete.text') }}</p>
          <v-alert v-if="deleteError" type="error" variant="tonal" class="mt-4">
            {{ t('renault.delete.error') }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="confirming = false">{{ t('renault.delete.cancel') }}</v-btn>
          <v-btn
            color="error"
            variant="flat"
            :loading="deletePending"
            class="confirm-delete"
            @click="confirmDelete"
          >
            {{ t('renault.delete.confirm') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-form v-if="open" id="renault-form" novalidate class="connect-form" @submit.prevent="submit">
      <ErrorSummary
        v-if="summary.length"
        ref="summaryRef"
        :title="t('renault.error.summary')"
        :items="summary"
      />

      <template v-if="sentTo === null">
        <v-alert type="info" variant="tonal" class="password-note mb-4">
          {{ t('renault.passwordNote') }}
        </v-alert>
        <v-text-field
          :id="emailId"
          v-model="email"
          :label="t('renault.email')"
          type="email"
          :error-messages="fieldError(emailId)"
          :aria-invalid="fieldError(emailId) ? 'true' : undefined"
          autocomplete="off"
          spellcheck="false"
          autocapitalize="none"
          class="value email mb-2"
        />
        <v-text-field
          :id="passwordId"
          v-model="password"
          :label="t('renault.password')"
          type="password"
          :error-messages="fieldError(passwordId)"
          :aria-invalid="fieldError(passwordId) ? 'true' : undefined"
          autocomplete="new-password"
          class="value password mb-2"
        />
        <p class="text-body-small text-medium-emphasis mb-3">{{ t('renault.help') }}</p>
        <div class="d-flex flex-wrap ga-3">
          <v-btn
            type="submit"
            color="primary"
            :prepend-icon="mdiLogin"
            :loading="saving"
            class="save"
          >
            {{ t('renault.save') }}
          </v-btn>
          <v-btn
            v-if="opened && current"
            variant="text"
            class="cancel"
            :disabled="saving"
            @click="close()"
          >
            {{ t('renault.cancel') }}
          </v-btn>
        </div>
      </template>

      <template v-else>
        <h3 class="text-title-medium mb-2">{{ t('renault.code.title') }}</h3>
        <p class="text-body-medium mb-3 code-sent">
          {{ t('renault.code.sent', { address: sentTo }) }}
        </p>
        <v-text-field
          :id="codeId"
          v-model="codeDraft"
          :label="t('renault.code.label')"
          inputmode="numeric"
          :error-messages="fieldError(codeId)"
          :aria-invalid="fieldError(codeId) ? 'true' : undefined"
          autocomplete="one-time-code"
          spellcheck="false"
          class="value code mb-2"
        />
        <p class="text-body-small text-medium-emphasis mb-3">{{ t('renault.code.help') }}</p>
        <div class="d-flex flex-wrap ga-3">
          <v-btn
            type="submit"
            color="primary"
            :prepend-icon="mdiSend"
            :loading="saving"
            class="send-code"
          >
            {{ t('renault.code.send') }}
          </v-btn>
          <v-btn variant="text" class="back" :disabled="saving" @click="back">
            {{ t('renault.code.back') }}
          </v-btn>
        </div>
      </template>
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
.value {
  max-width: 32rem;
}
</style>
