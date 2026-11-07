<script setup lang="ts">
import { mdiDelete, mdiKeyVariant, mdiOpenInNew } from '@mdi/js'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ConnectionSettings } from '@/entities/connection'
import { isApiError } from '@/shared/api'
import { useFormat, useNow } from '@/shared/lib'
import { ErrorSummary, FactItem } from '@/shared/ui'
import { useBmwPolling } from '../composables/useBmwPolling'
import { useDeleteBmw } from '../composables/useDeleteBmw'
import { useStartBmwAuthorization } from '../composables/useStartBmwAuthorization'
import { clientIdId, validate, type Issue, type Rule } from '../model/validate'

// BmwForm connects the account's BMW CarData (ADR 0045): BMW's official service for the
// owner of a BMW or a MINI. The owner creates a client ID in the MyBMW or MyMINI portal
// (the steps, and the guide), gives it here, and Runsten starts BMW's device flow with it:
// a code, which the owner approves on BMW's own page, signed in with their BMW account.
// Runsten asks BMW until they did (useBmwPolling), and never sees a password. The client
// ID is never shown again: its last four characters, and when it was connected. Without a
// connection, the form is open; with one, it is replaced behind a button.
const props = defineProps<{ connection: ConnectionSettings }>()
const { t } = useI18n()
const f = useFormat()
const now = useNow()
const authorization = useStartBmwAuthorization()
const polling = useBmwPolling()

const current = computed(() => props.connection.bmw)
const opened = ref(false)
const open = computed(() => opened.value || !current.value)
const draft = ref('')
const sent = ref(false)
const status = ref('')
const waiting = computed(() => polling.phase.value === 'waiting' && !!authorization.device.value)
const device = computed(() => authorization.device.value)

watch(draft, () => {
  status.value = ''
  authorization.reset()
  if (polling.phase.value === 'failed') polling.cancel()
})

const checked = computed(() => validate(draft.value))
const issue = computed(() => (sent.value ? checked.value.issue : null))
const messages: Record<Rule, (p: Record<string, number>) => string> = {
  required: () => t('bmw.error.required'),
  spaces: () => t('bmw.error.spaces'),
  characters: () => t('bmw.error.characters'),
  tooShort: (p) => t('bmw.error.tooShort', { min: p.min ?? 0 }),
  tooLong: (p) => t('bmw.error.tooLong', { max: p.max ?? 0 }),
}
const message = (i: Issue) => messages[i.rule](i.params ?? {})

// What the API refused, at the start of the flow or while waiting for the approval.
const serverError = computed<{ field?: string; text: string } | null>(() => {
  const e: unknown = authorization.error.value ?? polling.error.value
  if (!e) return null
  if (isApiError(e, 'credential_refused'))
    return authorization.error.value
      ? { field: clientIdId, text: t('bmw.error.clientRefused') }
      : { text: t('bmw.error.refused') }
  if (isApiError(e, 'invalid_body')) return { field: clientIdId, text: t('bmw.error.invalidBody') }
  if (isApiError(e, 'authorization_expired')) return { text: t('bmw.error.expired') }
  if (isApiError(e, 'scope_missing')) return { text: t('bmw.error.scopeMissing') }
  if (isApiError(e, 'no_vehicle')) return { text: t('bmw.error.noVehicle') }
  if (isApiError(e, 'too_many_vehicles')) return { text: t('bmw.error.tooManyVehicles') }
  if (isApiError(e, 'not_admitted')) return { text: t('bmw.error.notAdmitted') }
  if (isApiError(e, 'feature_unavailable')) return { text: t('bmw.error.notOffered') }
  if (isApiError(e, 'source_unavailable'))
    return {
      text: e.status === 503 ? t('bmw.error.tooManyPending') : t('bmw.error.bmwUnavailable'),
    }
  if (isApiError(e, 'unavailable') || isApiError(e, 'network'))
    return { text: t('bmw.error.unavailable') }
  return { text: t('bmw.error.internal') }
})
const fieldError = computed(() => {
  if (issue.value) return message(issue.value)
  return serverError.value?.field ? serverError.value.text : undefined
})
const summary = computed(() => [
  ...(issue.value ? [{ field: clientIdId, text: message(issue.value) }] : []),
  ...(serverError.value ? [serverError.value] : []),
])
const summaryRef = ref<InstanceType<typeof ErrorSummary> | null>(null)
async function focusSummary() {
  await nextTick()
  summaryRef.value?.focus()
}

const toggleId = 'bmw-toggle'
const openId = 'bmw-open'
// Opened on request, the form focuses its field once shown: no autofocus (Vuetify's
// takes the focus back 50 ms later).
async function openForm() {
  draft.value = ''
  sent.value = false
  opened.value = true
  await nextTick()
  authorization.reset()
  polling.cancel()
  status.value = ''
  document.getElementById(clientIdId)?.focus()
}

async function close(said = '') {
  opened.value = false
  draft.value = ''
  sent.value = false
  authorization.reset()
  await nextTick()
  document.getElementById(toggleId)?.focus()
  status.value = said
}

async function submit() {
  sent.value = true
  const value = checked.value.clientId
  if (!value) return focusSummary()
  status.value = ''
  try {
    polling.begin(await authorization.start(value))
    // The link to BMW's page is what the owner does next.
    await nextTick()
    document.getElementById(openId)?.focus()
  } catch {
    await focusSummary()
  }
}

async function cancel() {
  polling.cancel()
  authorization.reset()
  await nextTick()
  document.getElementById(clientIdId)?.focus()
  status.value = t('bmw.cancelled')
}

// The wait ends: connected, or a terminal answer to tell.
watch(polling.phase, async (p) => {
  if (p === 'connected') await close(t('bmw.saved'))
  else if (p === 'failed') {
    authorization.reset()
    sent.value = false
    await focusSummary()
  }
})
// The status line tells the wait too: a live region that is always in the page.
const statusText = computed(() => {
  if (!waiting.value) return status.value
  return polling.unreachable.value ? t('bmw.waitingUnreachable') : t('bmw.waiting')
})

const { remove, isPending: deletePending, error: deleteError, reset: resetDelete } = useDeleteBmw()
const confirming = ref(false)
const deletedNow = ref(false)
const deleteId = 'bmw-delete'

function askDelete() {
  status.value = ''
  resetDelete()
  confirming.value = true
}

async function confirmDelete() {
  try {
    await remove()
    // The form opens at once, for the next connection: the connection is read again later.
    opened.value = true
    deletedNow.value = true
    confirming.value = false
  } catch {
    // Shown from the error, in the dialog.
  }
}

// Once the dialog closes, the focus goes back to its button; once the connection is
// removed, that button is gone, and the form open: to its field.
watch(confirming, async (v) => {
  if (v) return
  await nextTick()
  if (!deletedNow.value) {
    document.getElementById(deleteId)?.focus()
    return
  }
  deletedNow.value = false
  document.getElementById(clientIdId)?.focus()
  status.value = t('bmw.deleted')
})

// BMW's page is the owner's: the one with the code filled in when BMW gives it.
const bmwPage = computed(
  () => device.value?.verification_uri_complete ?? device.value?.verification_uri,
)
const guide = new URL('https://runsten.app/docs/guide/bmw')
</script>

<template>
  <div class="bmw-form">
    <p class="text-body-medium mb-1">{{ t('bmw.intro') }}</p>
    <p class="text-body-medium mb-3">
      {{ t('bmw.beta') }}
      <a :href="guide.href" target="_blank" rel="noreferrer" class="guide">
        {{ t('bmw.guide') }}<v-icon :icon="mdiOpenInNew" size="x-small" class="ms-1" />
        <span class="d-sr-only">{{ t('bmw.newTab') }}</span>
      </a>
    </p>

    <v-alert
      v-if="current?.refused_at"
      type="error"
      variant="tonal"
      :title="t('bmw.refused.title')"
      class="refused mb-4"
    >
      {{ t('bmw.refused.body', { time: f.dateTime(current.refused_at, now) }) }}
    </v-alert>

    <dl v-if="current" class="facts mb-4">
      <FactItem :label="t('bmw.current')" :value="t('bmw.last4', { last4: current.last4 })" />
      <FactItem :label="t('bmw.setAt')" :value="f.dateTime(current.set_at, now)" />
    </dl>

    <div v-if="!open || current" class="d-flex flex-wrap ga-2 mb-4">
      <v-btn
        v-if="!open"
        :id="toggleId"
        :prepend-icon="mdiKeyVariant"
        :color="current?.refused_at ? 'primary' : undefined"
        variant="tonal"
        class="toggle"
        @click="openForm"
      >
        {{ t('bmw.reconnect') }}
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
        {{ t('bmw.delete.button') }}
      </v-btn>
    </div>

    <v-dialog v-model="confirming" max-width="28rem" aria-labelledby="delete-bmw-title">
      <v-card>
        <v-card-title id="delete-bmw-title" tag="h2" class="text-title-large text-wrap">
          {{ t('bmw.delete.title') }}
        </v-card-title>
        <v-card-text>
          <p class="text-body-medium">{{ t('bmw.delete.text') }}</p>
          <v-alert v-if="deleteError" type="error" variant="tonal" class="mt-4">
            {{ t('bmw.delete.error') }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="confirming = false">{{ t('bmw.delete.cancel') }}</v-btn>
          <v-btn
            color="error"
            variant="flat"
            :loading="deletePending"
            class="confirm-delete"
            @click="confirmDelete"
          >
            {{ t('bmw.delete.confirm') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <section
      v-if="open && waiting && device"
      class="approval mb-4"
      aria-labelledby="bmw-code-title"
    >
      <h3 id="bmw-code-title" class="text-title-medium mb-2">{{ t('bmw.code.title') }}</h3>
      <p class="text-body-medium mb-2">{{ t('bmw.code.open') }}</p>
      <p class="text-body-medium mb-1">{{ t('bmw.code.label') }}</p>
      <p class="user-code text-headline-medium mb-3">{{ device.user_code }}</p>
      <p class="mb-3">
        <a
          :id="openId"
          :href="bmwPage"
          target="_blank"
          rel="noreferrer"
          class="bmw-page text-body-medium"
        >
          {{ t('bmw.code.page') }}<v-icon :icon="mdiOpenInNew" size="x-small" class="ms-1" />
          <span class="d-sr-only">{{ t('bmw.newTab') }}</span>
        </a>
      </p>
      <p class="expires text-body-medium text-medium-emphasis mb-3">
        {{ t('bmw.code.expires', { time: f.dateTime(device.expires_at, now) }) }}
      </p>
      <v-btn variant="text" class="cancel-code" @click="cancel">{{ t('bmw.code.cancel') }}</v-btn>
    </section>

    <v-form
      v-if="open && !waiting"
      id="bmw-form"
      novalidate
      class="connect-form"
      @submit.prevent="submit"
    >
      <ErrorSummary
        v-if="summary.length"
        ref="summaryRef"
        :title="t('bmw.error.summary')"
        :items="summary"
      />
      <h3 class="text-title-medium mb-2">{{ t('bmw.steps.title') }}</h3>
      <ol class="steps text-body-medium mb-4">
        <li>{{ t('bmw.steps.create') }}</li>
        <li>{{ t('bmw.steps.subscribe') }}</li>
        <li>{{ t('bmw.steps.stream') }}</li>
        <li>{{ t('bmw.steps.note') }}</li>
      </ol>
      <v-text-field
        :id="clientIdId"
        v-model="draft"
        :label="t('bmw.label')"
        :error-messages="fieldError"
        :aria-invalid="fieldError ? 'true' : undefined"
        autocomplete="off"
        spellcheck="false"
        autocapitalize="none"
        class="value mb-2"
      />
      <p class="text-body-small text-medium-emphasis mb-3">{{ t('bmw.help') }}</p>
      <div class="d-flex flex-wrap ga-3">
        <v-btn
          type="submit"
          color="primary"
          :prepend-icon="mdiKeyVariant"
          :loading="authorization.isPending.value"
          class="save"
        >
          {{ t('bmw.save') }}
        </v-btn>
        <v-btn v-if="opened && current" variant="text" class="cancel" @click="close()">
          {{ t('bmw.cancel') }}
        </v-btn>
      </div>
    </v-form>
    <p role="status" class="status text-body-medium mt-2">{{ statusText }}</p>
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
.user-code {
  letter-spacing: 0.15em;
  font-weight: 500;
  overflow-wrap: anywhere;
}
</style>
