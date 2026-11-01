<script setup lang="ts">
import { mdiDelete, mdiLogin, mdiOpenInNew } from '@mdi/js'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ConnectionSettings } from '@/entities/connection'
import { isApiError } from '@/shared/api'
import { useFormat, useNow } from '@/shared/lib'
import { ErrorSummary, FactItem } from '@/shared/ui'
import { useConnectPolestarId } from '../composables/useConnectPolestarId'
import { useDeletePolestarId } from '../composables/useDeletePolestarId'
import { usePolestarIdSignIn } from '../composables/usePolestarIdSignIn'
import {
  callbackId,
  emailId,
  passwordId,
  startId,
  validateCallback,
  validatePassword,
  type Issue,
} from '../model/validate'

// PolestarIdForm connects the account's Polestar ID (ADR 0042): the Polestar app's API,
// unofficial, for the Polestars the Data Portal does not read (outside the EU/EEA). The
// owner chooses how:
//
//   - token (recommended): Runsten gives the address of Polestar's sign-in page; the owner
//     signs in there, with the browser's developer tools open, polestar.com shows an error
//     page, and the owner pastes back the address of the sign-in-callback the network log
//     holds. Runsten keeps the refresh token only.
//   - password: Runsten signs in as the app does, and keeps the email and password, sealed,
//     to sign in again. The form says so, and that Polestar's terms forbid giving them to
//     a third party: the self-hoster gives them to their own instance.
//
// Neither is shown again: the mode and when. Without a connection, the form is open; with
// one, it is replaced behind a button.
const props = defineProps<{ connection: ConnectionSettings }>()
const { t } = useI18n()
const f = useFormat()
const now = useNow()
const { connect, isPending, error, reset } = useConnectPolestarId()
const signIn = usePolestarIdSignIn()

const current = computed(() => props.connection.polestar_id)
const opened = ref(false)
const open = computed(() => opened.value || !current.value)
type Mode = 'token' | 'password'
const mode = ref<Mode>('token')
const callback = ref('')
const email = ref('')
const password = ref('')
const sent = ref(false)
const status = ref('')

watch([mode, callback, email, password], () => {
  status.value = ''
  reset()
})
watch(mode, () => {
  sent.value = false
})

const issues = computed<Issue[]>(() => {
  if (!sent.value) return []
  if (mode.value === 'token')
    return validateCallback(callback.value, !!signIn.authorization.value).issues
  return validatePassword(email.value, password.value).issues
})
const message = (i: Issue) => {
  switch (i.rule) {
    case 'start':
      return t('polestarId.error.start')
    case 'required':
      if (i.field === callbackId) return t('polestarId.error.callbackRequired')
      if (i.field === emailId) return t('polestarId.error.emailRequired')
      return t('polestarId.error.passwordRequired')
    case 'noCode':
      return t('polestarId.error.noCode')
    case 'email':
      return t('polestarId.error.email')
    case 'tooLong':
      return t('polestarId.error.tooLong', { max: i.params?.max ?? 0 })
  }
}
const serverError = computed<{ field?: string; text: string } | null>(() => {
  const e = error.value
  if (!e) return null
  const token = mode.value === 'token'
  if (isApiError(e, 'credential_refused'))
    return token
      ? { field: callbackId, text: t('polestarId.error.codeRefused') }
      : { field: passwordId, text: t('polestarId.error.passwordRefused') }
  if (isApiError(e, 'authorization_expired'))
    return { field: startId, text: t('polestarId.error.expired') }
  if (isApiError(e, 'invalid_body'))
    return token
      ? { field: callbackId, text: t('polestarId.error.noCode') }
      : { field: emailId, text: t('polestarId.error.invalidBody') }
  if (isApiError(e, 'no_vehicle')) return { text: t('polestarId.error.noVehicle') }
  if (isApiError(e, 'too_many_vehicles')) return { text: t('polestarId.error.tooManyVehicles') }
  if (isApiError(e, 'not_admitted')) return { text: t('polestarId.error.notAdmitted') }
  if (isApiError(e, 'feature_unavailable')) return { text: t('polestarId.error.notOffered') }
  if (isApiError(e, 'source_unavailable'))
    return { text: t('polestarId.error.polestarUnavailable') }
  if (isApiError(e, 'unavailable') || isApiError(e, 'network'))
    return { text: t('polestarId.error.unavailable') }
  return { text: t('polestarId.error.internal') }
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

const signInError = computed(() =>
  signIn.error.value
    ? isApiError(signIn.error.value, 'source_unavailable')
      ? t('polestarId.error.tooManyPending')
      : t('polestarId.error.unavailable')
    : '',
)
// The address of Polestar's page, a link the owner opens: a window opened after the
// request would be a popup the browser blocks.
async function start() {
  status.value = ''
  reset()
  try {
    await signIn.start()
  } catch {
    // Told from the error, beside the button.
  }
}

const toggleId = 'polestar-id-toggle'
async function openForm() {
  callback.value = email.value = password.value = ''
  sent.value = false
  opened.value = true
  await nextTick()
  reset()
  status.value = ''
  document.getElementById(`polestar-id-mode-${mode.value}`)?.focus()
}

async function close(said = '') {
  opened.value = false
  callback.value = email.value = password.value = ''
  sent.value = false
  signIn.reset()
  await nextTick()
  document.getElementById(toggleId)?.focus()
  status.value = said
}

async function submit() {
  sent.value = true
  const a = signIn.authorization.value
  try {
    if (mode.value === 'token') {
      const { callback: value } = validateCallback(callback.value, !!a)
      if (!value || !a) return await focusSummary()
      await connect({ mode: 'token', state: a.state, callback: value })
    } else {
      const { body } = validatePassword(email.value, password.value)
      if (!body) return await focusSummary()
      await connect({ mode: 'password', ...body })
    }
    await close(t('polestarId.saved'))
  } catch (e) {
    // A sign-in is spent once its code is tried: a new one is asked for.
    if (mode.value === 'token' && !isApiError(e, 'invalid_body')) {
      signIn.reset()
      // The server's refusal says to start again: not the start's own message too.
      sent.value = false
    }
    await focusSummary()
  }
}

const {
  remove,
  isPending: deletePending,
  error: deleteError,
  reset: resetDelete,
} = useDeletePolestarId()
const confirming = ref(false)
const deletedNow = ref(false)
const deleteId = 'polestar-id-delete'

function askDelete() {
  status.value = ''
  resetDelete()
  confirming.value = true
}

async function confirmDelete() {
  try {
    await remove()
    opened.value = true
    deletedNow.value = true
    confirming.value = false
  } catch {
    // Shown from the error, in the dialog.
  }
}

// Once the dialog closes, the focus goes back to its button; once removed, that button is
// gone, and the form open: to its first choice.
watch(confirming, async (v) => {
  if (v) return
  await nextTick()
  if (!deletedNow.value) {
    document.getElementById(deleteId)?.focus()
    return
  }
  deletedNow.value = false
  document.getElementById(`polestar-id-mode-${mode.value}`)?.focus()
  status.value = t('polestarId.deleted')
})

const guide = new URL('https://runsten.app/docs/guide/polestar-id')
</script>

<template>
  <div class="polestar-id-form">
    <p class="text-body-medium mb-1">{{ t('polestarId.intro') }}</p>
    <p class="text-body-medium mb-3">
      {{ t('polestarId.beta') }}
      <a :href="guide.href" target="_blank" rel="noreferrer" class="guide">
        {{ t('polestarId.guide') }}<v-icon :icon="mdiOpenInNew" size="x-small" class="ms-1" />
        <span class="d-sr-only">{{ t('polestarId.newTab') }}</span>
      </a>
    </p>

    <v-alert
      v-if="current?.refused_at"
      type="error"
      variant="tonal"
      :title="t('polestarId.refused.title')"
      class="refused mb-4"
    >
      {{ t('polestarId.refused.body', { time: f.dateTime(current.refused_at, now) }) }}
    </v-alert>

    <dl v-if="current" class="facts mb-4">
      <FactItem
        :label="t('polestarId.mode')"
        :value="
          current.mode === 'password' ? t('polestarId.modes.password') : t('polestarId.modes.token')
        "
      />
      <FactItem :label="t('polestarId.setAt')" :value="f.dateTime(current.set_at, now)" />
    </dl>

    <div v-if="!open || current" class="d-flex flex-wrap ga-2 mb-4">
      <v-btn
        v-if="!open"
        :id="toggleId"
        :prepend-icon="mdiLogin"
        :color="current?.refused_at ? 'primary' : undefined"
        variant="tonal"
        class="toggle"
        @click="openForm"
      >
        {{ t('polestarId.reconnect') }}
      </v-btn>
      <v-btn
        v-if="current"
        :id="deleteId"
        :prepend-icon="mdiDelete"
        variant="text"
        color="error"
        class="delete-connection"
        @click="askDelete"
      >
        {{ t('polestarId.delete.button') }}
      </v-btn>
    </div>

    <v-dialog v-model="confirming" max-width="28rem" aria-labelledby="delete-polestar-id-title">
      <v-card>
        <v-card-title id="delete-polestar-id-title" tag="h2" class="text-title-large text-wrap">
          {{ t('polestarId.delete.title') }}
        </v-card-title>
        <v-card-text>
          <p class="text-body-medium">{{ t('polestarId.delete.text') }}</p>
          <v-alert v-if="deleteError" type="error" variant="tonal" class="mt-4">
            {{ t('polestarId.delete.error') }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="confirming = false">
            {{ t('polestarId.delete.cancel') }}
          </v-btn>
          <v-btn
            color="error"
            variant="flat"
            :loading="deletePending"
            class="confirm-delete"
            @click="confirmDelete"
          >
            {{ t('polestarId.delete.confirm') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-form
      v-if="open"
      id="polestar-id-form"
      novalidate
      class="connect-form"
      @submit.prevent="submit"
    >
      <ErrorSummary
        v-if="summary.length"
        ref="summaryRef"
        :title="t('polestarId.error.summary')"
        :items="summary"
      />
      <fieldset class="modes mb-4">
        <legend class="text-title-small mb-2">{{ t('polestarId.modes.legend') }}</legend>
        <v-radio-group v-model="mode" hide-details>
          <v-radio id="polestar-id-mode-token" value="token" class="mode-token">
            <template #label>{{ t('polestarId.modes.tokenChoice') }}</template>
          </v-radio>
          <v-radio id="polestar-id-mode-password" value="password" class="mode-password">
            <template #label>{{ t('polestarId.modes.passwordChoice') }}</template>
          </v-radio>
        </v-radio-group>
      </fieldset>

      <template v-if="mode === 'token'">
        <h3 class="text-title-medium mb-2">{{ t('polestarId.steps.title') }}</h3>
        <ol class="steps text-body-medium mb-4">
          <li>
            <p class="mb-2">{{ t('polestarId.steps.open') }}</p>
            <a
              v-if="signIn.authorization.value"
              :href="signIn.authorization.value.url"
              target="_blank"
              rel="noreferrer"
              class="sign-in-page"
            >
              {{ t('polestarId.steps.page')
              }}<v-icon :icon="mdiOpenInNew" size="x-small" class="ms-1" />
              <span class="d-sr-only">{{ t('polestarId.newTab') }}</span>
            </a>
            <v-btn
              v-else
              :id="startId"
              :loading="signIn.isPending.value"
              variant="tonal"
              class="start"
              @click="start"
            >
              {{ t('polestarId.steps.start') }}
            </v-btn>
            <p v-if="signInError" class="text-body-small text-error mt-1 start-error">
              {{ signInError }}
            </p>
          </li>
          <li>{{ t('polestarId.steps.devtools') }}</li>
          <li>{{ t('polestarId.steps.signIn') }}</li>
          <li>{{ t('polestarId.steps.copy') }}</li>
        </ol>
        <v-text-field
          :id="callbackId"
          v-model="callback"
          :label="t('polestarId.callback')"
          :error-messages="fieldError(callbackId)"
          :aria-invalid="fieldError(callbackId) ? 'true' : undefined"
          autocomplete="off"
          spellcheck="false"
          autocapitalize="none"
          class="value callback mb-2"
        />
        <p class="text-body-small text-medium-emphasis mb-3">{{ t('polestarId.tokenHelp') }}</p>
      </template>

      <template v-else>
        <v-alert type="warning" variant="tonal" class="password-warning mb-4">
          {{ t('polestarId.passwordWarning') }}
        </v-alert>
        <v-text-field
          :id="emailId"
          v-model="email"
          :label="t('polestarId.email')"
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
          :label="t('polestarId.password')"
          type="password"
          :error-messages="fieldError(passwordId)"
          :aria-invalid="fieldError(passwordId) ? 'true' : undefined"
          autocomplete="new-password"
          class="value password mb-2"
        />
      </template>

      <p class="text-body-small text-medium-emphasis mb-3">{{ t('polestarId.dataPortal') }}</p>
      <div class="d-flex flex-wrap ga-3">
        <v-btn
          type="submit"
          color="primary"
          :prepend-icon="mdiLogin"
          :loading="isPending"
          class="save"
        >
          {{ t('polestarId.save') }}
        </v-btn>
        <v-btn v-if="opened && current" variant="text" class="cancel" @click="close()">
          {{ t('polestarId.cancel') }}
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
.modes {
  border: 0;
  padding: 0;
  margin: 0;
}
.steps {
  padding-inline-start: 1.5rem;
}
.steps li + li {
  margin-top: 0.5rem;
}
.value {
  max-width: 32rem;
}
</style>
