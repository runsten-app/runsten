<script setup lang="ts">
import { mdiContentSave, mdiDelete, mdiKeyVariant, mdiOpenInNew } from '@mdi/js'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ConnectionSettings } from '@/entities/connection'
import { isApiError } from '@/shared/api'
import { useFormat, useNow } from '@/shared/lib'
import { ErrorSummary, FactItem } from '@/shared/ui'
import { useDeleteTeslaToken } from '../composables/useDeleteTeslaToken'
import { useSetTeslaToken } from '../composables/useSetTeslaToken'
import { tokenId, validate, type Issue, type Rule } from '../model/validate'

// TeslaForm gives the account's refresh token of Tesla's Owner API, the unofficial API of
// the Tesla app, as TeslaMate takes it: the owner gets it with a tool such as Tesla Auth,
// and Runsten never asks for the Tesla account's password. The steps lead to the tools and
// to the guide. The token is never shown again: the last four characters of the one given,
// and when it was given. Without one, the form is open; with one, it is replaced behind a
// button.
const props = defineProps<{ connection: ConnectionSettings }>()
const { t } = useI18n()
const f = useFormat()
const now = useNow()
const { setToken, isPending, error, reset } = useSetTeslaToken()

const token = computed(() => props.connection.tesla)
const opened = ref(false)
const open = computed(() => opened.value || !token.value)
const draft = ref('')
const sent = ref(false)
const status = ref('')

watch(draft, () => {
  status.value = ''
  reset()
})

const checked = computed(() => validate(draft.value))
const issue = computed(() => (sent.value ? checked.value.issue : null))
const messages: Record<Rule, (p: Record<string, number>) => string> = {
  required: () => t('tesla.error.required'),
  spaces: () => t('tesla.error.spaces'),
  characters: () => t('tesla.error.characters'),
  tooShort: (p) => t('tesla.error.tooShort', { min: p.min ?? 0 }),
  tooLong: (p) => t('tesla.error.tooLong', { max: p.max ?? 0 }),
}
const message = (i: Issue) => messages[i.rule](i.params ?? {})
const serverError = computed(() => {
  const e = error.value
  if (!e) return null
  if (isApiError(e, 'credential_refused')) return { field: tokenId, text: t('tesla.error.refused') }
  if (isApiError(e, 'invalid_body')) return { field: tokenId, text: t('tesla.error.invalidBody') }
  if (isApiError(e, 'api_closed')) return { text: t('tesla.error.apiClosed') }
  if (isApiError(e, 'no_vehicle')) return { text: t('tesla.error.noVehicle') }
  if (isApiError(e, 'too_many_vehicles')) return { text: t('tesla.error.tooManyVehicles') }
  if (isApiError(e, 'not_admitted')) return { text: t('tesla.error.notAdmitted') }
  if (isApiError(e, 'source_unavailable')) return { text: t('tesla.error.teslaUnavailable') }
  if (isApiError(e, 'unavailable') || isApiError(e, 'network'))
    return { text: t('tesla.error.unavailable') }
  return { text: t('tesla.error.internal') }
})
const fieldError = computed(() => {
  if (issue.value) return message(issue.value)
  return serverError.value?.field ? serverError.value.text : undefined
})
const summary = computed(() => [
  ...(issue.value ? [{ field: tokenId, text: message(issue.value) }] : []),
  ...(serverError.value ? [serverError.value] : []),
])
const summaryRef = ref<InstanceType<typeof ErrorSummary> | null>(null)
async function focusSummary() {
  await nextTick()
  summaryRef.value?.focus()
}

const toggleId = 'tesla-toggle'
// Opened on request, the form focuses its field once shown: no autofocus (Vuetify's
// takes the focus back 50 ms later).
async function openForm() {
  draft.value = ''
  sent.value = false
  opened.value = true
  await nextTick()
  reset()
  status.value = ''
  document.getElementById(tokenId)?.focus()
}

async function close(said = '') {
  opened.value = false
  draft.value = ''
  sent.value = false
  await nextTick()
  document.getElementById(toggleId)?.focus()
  status.value = said
}

async function submit() {
  sent.value = true
  const value = checked.value.token
  if (!value) return focusSummary()
  try {
    await setToken(value)
    await close(t('tesla.saved'))
  } catch {
    await focusSummary()
  }
}

const {
  deleteToken,
  isPending: deletePending,
  error: deleteError,
  reset: resetDelete,
} = useDeleteTeslaToken()
const confirming = ref(false)
const deletedNow = ref(false)
const deleteId = 'tesla-delete'

function askDelete() {
  status.value = ''
  resetDelete()
  confirming.value = true
}

async function confirmDelete() {
  try {
    await deleteToken()
    // The form opens at once, for the next token: the connection is read again later.
    opened.value = true
    deletedNow.value = true
    confirming.value = false
  } catch {
    // Shown from the error, in the dialog.
  }
}

// Once the dialog closes, the focus goes back to its button; once the token is deleted,
// that button is gone, and the form open: to its field.
watch(confirming, async (v) => {
  if (v) return
  await nextTick()
  if (!deletedNow.value) {
    document.getElementById(deleteId)?.focus()
    return
  }
  deletedNow.value = false
  document.getElementById(tokenId)?.focus()
  status.value = t('tesla.deleted')
})

// Tesla Auth, the tool TeslaMate's author publishes for its tokens, and Runsten's guide:
// their names are the same in every language.
const tool = { name: 'Tesla Auth', href: new URL('https://github.com/adriankumpf/tesla_auth').href }
const guide = new URL('https://runsten.app/docs/guide/tesla')
</script>

<template>
  <div class="tesla-form">
    <p class="text-body-medium mb-1">
      {{ t('tesla.intro') }}
    </p>
    <p class="text-body-medium mb-3">
      {{ t('tesla.beta') }}
      <a :href="guide.href" target="_blank" rel="noreferrer" class="guide">
        {{ t('tesla.guide') }}<v-icon :icon="mdiOpenInNew" size="x-small" class="ms-1" />
        <span class="d-sr-only">{{ t('tesla.steps.newTab') }}</span>
      </a>
    </p>

    <v-alert
      v-if="token?.refused_at"
      type="error"
      variant="tonal"
      :title="t('tesla.refused.title')"
      class="refused mb-4"
    >
      {{ t('tesla.refused.body', { time: f.dateTime(token.refused_at, now) }) }}
    </v-alert>

    <dl v-if="token" class="facts mb-4">
      <FactItem :label="t('tesla.current')" :value="t('tesla.last4', { last4: token.last4 })" />
      <FactItem :label="t('tesla.setAt')" :value="f.dateTime(token.set_at, now)" />
    </dl>

    <div v-if="!open || token" class="d-flex flex-wrap ga-2 mb-4">
      <v-btn
        v-if="!open"
        :id="toggleId"
        :prepend-icon="mdiKeyVariant"
        :color="token?.refused_at ? 'primary' : undefined"
        variant="tonal"
        class="toggle"
        @click="openForm"
      >
        {{ t('tesla.replace') }}
      </v-btn>
      <v-btn
        v-if="token"
        :id="deleteId"
        :prepend-icon="mdiDelete"
        variant="text"
        color="error"
        class="delete-token"
        @click="askDelete"
      >
        {{ t('tesla.delete.button') }}
      </v-btn>
    </div>

    <v-dialog v-model="confirming" max-width="28rem" aria-labelledby="delete-tesla-title">
      <v-card>
        <v-card-title id="delete-tesla-title" tag="h2" class="text-title-large text-wrap">
          {{ t('tesla.delete.title') }}
        </v-card-title>
        <v-card-text>
          <p class="text-body-medium">
            {{ t('tesla.delete.text') }}
          </p>
          <v-alert v-if="deleteError" type="error" variant="tonal" class="mt-4">
            {{ t('tesla.delete.error') }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="confirming = false">{{ t('tesla.delete.cancel') }}</v-btn>
          <v-btn
            color="error"
            variant="flat"
            :loading="deletePending"
            class="confirm-delete"
            @click="confirmDelete"
          >
            {{ t('tesla.delete.confirm') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-form v-if="open" id="tesla-form" novalidate class="token-form" @submit.prevent="submit">
      <ErrorSummary
        v-if="summary.length"
        ref="summaryRef"
        :title="t('tesla.error.summary')"
        :items="summary"
      />
      <h3 class="text-title-medium mb-2">{{ t('tesla.steps.title') }}</h3>
      <ol class="steps text-body-medium mb-4">
        <li>
          {{ t('tesla.steps.tool') }}
          <a :href="tool.href" target="_blank" rel="noreferrer" class="tool">
            {{ tool.name }}<v-icon :icon="mdiOpenInNew" size="x-small" class="ms-1" />
            <span class="d-sr-only">{{ t('tesla.steps.newTab') }}</span>
          </a>
          {{ t('tesla.steps.phone') }}
        </li>
        <li>{{ t('tesla.steps.signIn') }}</li>
        <li>{{ t('tesla.steps.copy') }}</li>
      </ol>
      <v-text-field
        :id="tokenId"
        v-model="draft"
        :label="t('tesla.label')"
        type="password"
        :error-messages="fieldError"
        :aria-invalid="fieldError ? 'true' : undefined"
        autocomplete="new-password"
        spellcheck="false"
        autocapitalize="none"
        class="value mb-2"
      />
      <p class="text-body-small text-medium-emphasis mb-3">{{ t('tesla.help') }}</p>
      <div class="d-flex flex-wrap ga-3">
        <v-btn
          type="submit"
          color="primary"
          :prepend-icon="mdiContentSave"
          :loading="isPending"
          class="save"
        >
          {{ t('tesla.save') }}
        </v-btn>
        <v-btn v-if="opened && token" variant="text" class="cancel" @click="close()">
          {{ t('tesla.cancel') }}
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
