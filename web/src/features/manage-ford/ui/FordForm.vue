<script setup lang="ts">
import { mdiDelete, mdiLogin, mdiOpenInNew } from '@mdi/js'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ConnectionSettings, Ford } from '@/entities/connection'
import { isApiError } from '@/shared/api'
import { useFormat, useNow } from '@/shared/lib'
import { ErrorSummary, FactItem } from '@/shared/ui'
import { useConnectFord } from '../composables/useConnectFord'
import { useDeleteFord } from '../composables/useDeleteFord'
import { useFordSignIn } from '../composables/useFordSignIn'
import { grantState } from '../model/grant'
import { leave } from '../model/leave'
import {
  callbackId,
  clientIdId,
  clientSecretId,
  validateApplication,
  validateCallback,
  type Issue,
} from '../model/validate'

// FordForm connects the account's Ford through FordConnect Query, Ford's own API: the
// owner creates an application on Ford's developer portal, with the redirect address the
// form tells, and gives its client ID and secret. Runsten then gives the address of
// Ford's sign-in, where they choose the car and consent:
//
//   - on an instance whose address Ford accepts (https, or localhost), the browser goes
//     there in this tab, and Ford sends it back to the Connection page with the outcome;
//   - else Ford sends it to a localhost address that does not load: the owner opens the
//     sign-in in another tab, and pastes back the address it ended on.
//
// Neither the client ID nor the secret is shown again: the client ID's last four
// characters, when the owner connected, and when Ford asks for a new sign-in (a year
// later) — the form warns two weeks before, and once it is over or was refused. Without
// a connection, the form is open; with one, it is replaced behind a button.
// off: the source is turned off for the account (the flag): a connection made before
// stays told and can be removed; none is made.
const props = defineProps<{ connection: ConnectionSettings; off?: boolean }>()
const { t } = useI18n()
const f = useFormat()
const now = useNow()
const signIn = useFordSignIn()
const { connect, isPending, error, reset } = useConnectFord()

const current = computed((): Ford | null => props.connection.ford ?? null)
const state = computed(() => (current.value ? grantState(current.value, now.value) : null))
const redirect = computed(() => props.connection.ford_application.redirect_uri)
const opened = ref(false)
const open = computed(() => !props.off && (opened.value || !current.value))
const clientId = ref('')
const clientSecret = ref('')
const callback = ref('')
const sent = ref(false)
const status = ref('')
// The sign-in started whose page the owner pastes back: none while Ford comes back by
// itself, or before the application is given.
const pasting = computed(() => {
  const a = signIn.authorization.value
  return a && !a.callback ? a : null
})

// An edit clears what the last attempt was told; not the form's own emptying of the
// address once a sign-in is spent, whose refusal is still to be read.
let spent = false
watch([clientId, clientSecret, callback], () => {
  status.value = ''
  if (spent) {
    spent = false
    return
  }
  reset()
})

const issues = computed<Issue[]>(() => {
  if (!sent.value) return []
  return pasting.value
    ? validateCallback(callback.value).issues
    : validateApplication(clientId.value, clientSecret.value).issues
})
const message = (i: Issue) => {
  switch (i.rule) {
    case 'required':
      if (i.field === clientIdId) return t('ford.error.clientIdRequired')
      if (i.field === clientSecretId) return t('ford.error.clientSecretRequired')
      return t('ford.error.callbackRequired')
    case 'characters':
      return t('ford.error.characters')
    case 'tooShort':
      return t('ford.error.tooShort', { min: i.params?.min ?? 0 })
    case 'tooLong':
      return t('ford.error.tooLong', { max: i.params?.max ?? 0 })
    case 'noCode':
      return t('ford.error.noCode')
  }
}
const serverError = computed<{ field?: string; text: string } | null>(() => {
  const e = error.value ?? signIn.error.value
  if (!e) return null
  if (isApiError(e, 'credential_refused')) return { text: t('ford.error.applicationRefused') }
  if (isApiError(e, 'authorization_expired')) return { text: t('ford.error.expired') }
  if (isApiError(e, 'invalid_body'))
    return pasting.value
      ? { field: callbackId, text: t('ford.error.noCode') }
      : { field: clientIdId, text: t('ford.error.invalidBody') }
  if (isApiError(e, 'no_vehicle')) return { text: t('ford.error.noVehicle') }
  if (isApiError(e, 'too_many_vehicles')) return { text: t('ford.error.tooManyVehicles') }
  if (isApiError(e, 'not_admitted')) return { text: t('ford.error.notAdmitted') }
  if (isApiError(e, 'feature_unavailable')) return { text: t('ford.error.notOffered') }
  if (isApiError(e, 'source_unavailable'))
    return {
      text: error.value ? t('ford.error.fordUnavailable') : t('ford.error.tooManyPending'),
    }
  if (isApiError(e, 'unavailable') || isApiError(e, 'network'))
    return { text: t('ford.error.unavailable') }
  return { text: t('ford.error.internal') }
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

const toggleId = 'ford-toggle'
function clear() {
  clientId.value = clientSecret.value = callback.value = ''
  sent.value = false
  signIn.reset()
}

async function openForm() {
  clear()
  opened.value = true
  await nextTick()
  reset()
  status.value = ''
  document.getElementById(clientIdId)?.focus()
}

async function close(said = '') {
  opened.value = false
  clear()
  await nextTick()
  document.getElementById(toggleId)?.focus()
  status.value = said
}

// startAgain goes back to the application, a sign-in spent or left: its fields are kept.
async function startAgain() {
  callback.value = ''
  sent.value = false
  signIn.reset()
  reset()
  await nextTick()
  document.getElementById(clientIdId)?.focus()
}

async function submit() {
  sent.value = true
  const a = pasting.value
  if (a) {
    const { callback: value } = validateCallback(callback.value)
    if (!value) return await focusSummary()
    try {
      await connect({ state: a.state, callback: value })
      await close(t('ford.saved'))
    } catch (e) {
      // A sign-in is spent once its code is tried: the owner starts again.
      if (!isApiError(e, 'invalid_body')) {
        signIn.reset()
        spent = callback.value !== ''
        callback.value = ''
        sent.value = false
      }
      await focusSummary()
    }
    return
  }
  const { app } = validateApplication(clientId.value, clientSecret.value)
  if (!app) return await focusSummary()
  reset()
  try {
    const started = await signIn.start(app)
    sent.value = false
    if (started.callback) {
      // Ford comes back to this instance, which ends the sign-in and tells its outcome.
      leave(started.url)
      return
    }
    await nextTick()
    document.getElementById(callbackId)?.focus()
  } catch {
    await focusSummary()
  }
}

const { remove, isPending: deletePending, error: deleteError, reset: resetDelete } = useDeleteFord()
const confirming = ref(false)
const deletedNow = ref(false)
const deleteId = 'ford-delete'

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
// gone, and the form open: to its first field.
watch(confirming, async (v) => {
  if (v) return
  await nextTick()
  if (!deletedNow.value) {
    document.getElementById(deleteId)?.focus()
    return
  }
  deletedNow.value = false
  document.getElementById(clientIdId)?.focus()
  status.value = t('ford.deleted')
})

const guide = new URL('https://runsten.app/docs/guide/ford')
const portal = new URL('https://developer.ford.com/developer-eu')
</script>

<template>
  <div class="ford-form">
    <p class="text-body-medium mb-1">{{ t('ford.intro') }}</p>
    <p class="text-body-medium mb-3">
      {{ t('ford.beta') }}
      <a :href="guide.href" target="_blank" rel="noreferrer" class="guide">
        {{ t('ford.guide') }}<v-icon :icon="mdiOpenInNew" size="x-small" class="ms-1" />
        <span class="d-sr-only">{{ t('ford.newTab') }}</span>
      </a>
    </p>

    <v-alert
      v-if="current && state === 'refused' && current.refused_at"
      type="error"
      variant="tonal"
      :title="t('ford.refused.title')"
      class="refused mb-4"
    >
      {{ t('ford.refused.body', { time: f.dateTime(current.refused_at, now) }) }}
    </v-alert>
    <v-alert
      v-else-if="current && state === 'expired' && current.expires_at"
      type="error"
      variant="tonal"
      :title="t('ford.expired.title')"
      class="expired mb-4"
    >
      {{ t('ford.expired.body', { date: f.day(current.expires_at) }) }}
    </v-alert>
    <v-alert
      v-else-if="current && state === 'soon' && current.expires_at"
      type="warning"
      variant="tonal"
      :title="t('ford.renew.title')"
      class="renew mb-4"
    >
      {{ t('ford.renew.body', { date: f.day(current.expires_at) }) }}
    </v-alert>

    <dl v-if="current" class="facts mb-4">
      <FactItem :label="t('ford.current')" :value="t('ford.last4', { last4: current.last4 })" />
      <FactItem :label="t('ford.setAt')" :value="f.dateTime(current.set_at, now)" />
      <FactItem
        :label="t('ford.expiresAt')"
        :value="current.expires_at ? f.day(current.expires_at) : t('ford.expiresUnknown')"
      />
    </dl>

    <div v-if="!open || current" class="d-flex flex-wrap ga-2 mb-4">
      <v-btn
        v-if="!open && !off"
        :id="toggleId"
        :prepend-icon="mdiLogin"
        :color="state && state !== 'valid' ? 'primary' : undefined"
        variant="tonal"
        class="toggle"
        @click="openForm"
      >
        {{ t('ford.reconnect') }}
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
        {{ t('ford.delete.button') }}
      </v-btn>
    </div>

    <v-dialog v-model="confirming" max-width="28rem" aria-labelledby="delete-ford-title">
      <v-card>
        <v-card-title id="delete-ford-title" tag="h2" class="text-title-large text-wrap">
          {{ t('ford.delete.title') }}
        </v-card-title>
        <v-card-text>
          <p class="text-body-medium">{{ t('ford.delete.text') }}</p>
          <v-alert v-if="deleteError" type="error" variant="tonal" class="mt-4">
            {{ t('ford.delete.error') }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="confirming = false">
            {{ t('ford.delete.cancel') }}
          </v-btn>
          <v-btn
            color="error"
            variant="flat"
            :loading="deletePending"
            class="confirm-delete"
            @click="confirmDelete"
          >
            {{ t('ford.delete.confirm') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-form v-if="open" id="ford-form" novalidate class="connect-form" @submit.prevent="submit">
      <ErrorSummary
        v-if="summary.length"
        ref="summaryRef"
        :title="t('ford.error.summary')"
        :items="summary"
      />

      <template v-if="!pasting">
        <h3 class="text-title-medium mb-2">{{ t('ford.steps.title') }}</h3>
        <ol class="steps text-body-medium mb-4">
          <li>
            {{ t('ford.steps.create') }}
            <a :href="portal.href" target="_blank" rel="noreferrer" class="portal">
              {{ t('ford.steps.portal')
              }}<v-icon :icon="mdiOpenInNew" size="x-small" class="ms-1" />
              <span class="d-sr-only">{{ t('ford.newTab') }}</span>
            </a>
          </li>
          <li>
            <p class="mb-2">{{ t('ford.steps.redirect') }}</p>
            <v-text-field
              id="ford-redirect"
              :model-value="redirect"
              :label="t('ford.redirect')"
              readonly
              hide-details
              spellcheck="false"
              class="value redirect"
              @focus="($event.target as HTMLInputElement).select()"
            />
          </li>
          <li>{{ t('ford.steps.wait') }}</li>
          <li>{{ t('ford.steps.credentials') }}</li>
        </ol>
        <v-text-field
          :id="clientIdId"
          v-model="clientId"
          :label="t('ford.clientId')"
          :error-messages="fieldError(clientIdId)"
          :aria-invalid="fieldError(clientIdId) ? 'true' : undefined"
          autocomplete="off"
          spellcheck="false"
          autocapitalize="none"
          class="value client-id mb-2"
        />
        <v-text-field
          :id="clientSecretId"
          v-model="clientSecret"
          :label="t('ford.clientSecret')"
          type="password"
          :error-messages="fieldError(clientSecretId)"
          :aria-invalid="fieldError(clientSecretId) ? 'true' : undefined"
          autocomplete="off"
          class="value client-secret mb-2"
        />
        <p class="text-body-small text-medium-emphasis mb-3">{{ t('ford.secretHelp') }}</p>
        <div class="d-flex flex-wrap ga-3">
          <v-btn
            type="submit"
            color="primary"
            :prepend-icon="mdiLogin"
            :loading="signIn.isPending.value"
            class="start"
          >
            {{ t('ford.start') }}
          </v-btn>
          <v-btn v-if="opened && current" variant="text" class="cancel" @click="close()">
            {{ t('ford.cancel') }}
          </v-btn>
        </div>
      </template>

      <template v-else>
        <h3 class="text-title-medium mb-2">{{ t('ford.paste.title') }}</h3>
        <ol class="steps text-body-medium mb-4">
          <li>
            {{ t('ford.paste.open') }}
            <a :href="pasting.url" target="_blank" rel="noreferrer" class="sign-in-page">
              {{ t('ford.paste.page') }}<v-icon :icon="mdiOpenInNew" size="x-small" class="ms-1" />
              <span class="d-sr-only">{{ t('ford.newTab') }}</span>
            </a>
          </li>
          <li>{{ t('ford.paste.explain', { redirect: pasting.redirect_uri }) }}</li>
          <li>{{ t('ford.paste.copy') }}</li>
        </ol>
        <v-text-field
          :id="callbackId"
          v-model="callback"
          :label="t('ford.callback')"
          :error-messages="fieldError(callbackId)"
          :aria-invalid="fieldError(callbackId) ? 'true' : undefined"
          autocomplete="off"
          spellcheck="false"
          autocapitalize="none"
          class="value callback mb-3"
        />
        <div class="d-flex flex-wrap ga-3">
          <v-btn
            type="submit"
            color="primary"
            :prepend-icon="mdiLogin"
            :loading="isPending"
            class="save"
          >
            {{ t('ford.save') }}
          </v-btn>
          <v-btn variant="text" class="start-again" @click="startAgain">
            {{ t('ford.startAgain') }}
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
