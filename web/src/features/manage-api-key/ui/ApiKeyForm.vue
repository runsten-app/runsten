<script setup lang="ts">
import { mdiContentSave, mdiDelete, mdiKeyVariant, mdiOpenInNew } from '@mdi/js'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ConnectionSettings } from '@/entities/connection'
import { isApiError } from '@/shared/api'
import { useFormat, useNow } from '@/shared/lib'
import { ErrorSummary, FactItem } from '@/shared/ui'
import { useDeleteApiKey } from '../composables/useDeleteApiKey'
import { useSetApiKey } from '../composables/useSetApiKey'
import { field, validate, type Issue, type Rule } from '../model/validate'

// ApiKeyForm gives the account's Volvo application key, on an instance without a key of
// its own (the hosted offer): Volvo counts its quota of calls on the key's application,
// so that the account's vehicles spend their own quota. The steps lead to Volvo's
// developer portal, where the user creates an application, unpublished, and copies its
// key. The key is never shown again: its last four characters and when it was given
// only. Without a key, the form is open: nothing is read without one; with one, it is
// replaced behind a button.
const props = defineProps<{ connection: ConnectionSettings }>()
const { t } = useI18n()
const f = useFormat()
const now = useNow()
const { setApiKey, isPending, error, reset } = useSetApiKey()

const key = computed(() => props.connection.api_key)
const opened = ref(false)
const open = computed(() => opened.value || !key.value)
const draft = ref('')
const sent = ref(false)
const status = ref('')

watch(draft, () => {
  status.value = ''
  reset()
})

const checked = computed(() => validate(draft.value))
const issues = computed(() => (sent.value ? checked.value.issues : []))
const messages: Record<Rule, (p: Record<string, number>) => string> = {
  required: () => t('apiKey.error.required'),
  spaces: () => t('apiKey.error.spaces'),
  characters: () => t('apiKey.error.characters'),
  tooShort: (p) => t('apiKey.error.tooShort', { min: p.min ?? 0 }),
  tooLong: (p) => t('apiKey.error.tooLong', { max: p.max ?? 0 }),
}
const message = (i: Issue) => messages[i.rule](i.params ?? {})
const fieldError = computed(() => {
  const i = issues.value[0]
  if (i) return message(i)
  return error.value && isApiError(error.value, 'api_key_refused')
    ? t('apiKey.error.refused')
    : undefined
})
const serverError = computed(() => {
  const e = error.value
  if (!e) return null
  if (isApiError(e, 'api_key_refused')) return { field, text: t('apiKey.error.refused') }
  if (isApiError(e, 'invalid_body')) return { field, text: t('apiKey.error.invalidBody') }
  if (isApiError(e, 'too_many_vehicles')) return { text: t('apiKey.error.tooManyVehicles') }
  if (isApiError(e, 'unavailable') || isApiError(e, 'network'))
    return { text: t('apiKey.error.unavailable') }
  return { text: t('apiKey.error.internal') }
})
const summary = computed(() => [
  ...issues.value.map((i) => ({ field: i.field, text: message(i) })),
  ...(serverError.value ? [serverError.value] : []),
])
const summaryRef = ref<InstanceType<typeof ErrorSummary> | null>(null)
async function focusSummary() {
  await nextTick()
  summaryRef.value?.focus()
}

const toggleId = 'api-key-toggle'
// Opened on request, the form focuses its field once shown: no autofocus (Vuetify's
// takes the focus back 50 ms later).
async function openForm() {
  draft.value = ''
  sent.value = false
  opened.value = true
  await nextTick()
  reset()
  status.value = ''
  document.getElementById(field)?.focus()
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
  const value = checked.value.key
  if (!value) return focusSummary()
  try {
    await setApiKey(value)
    await close(t('apiKey.saved'))
  } catch {
    await focusSummary()
  }
}

const {
  deleteApiKey,
  isPending: deletePending,
  error: deleteError,
  reset: resetDelete,
} = useDeleteApiKey()
const confirming = ref(false)
const deletedNow = ref(false)
const deleteId = 'api-key-delete'

function askDelete() {
  status.value = ''
  resetDelete()
  confirming.value = true
}

async function confirmDelete() {
  try {
    await deleteApiKey()
    // The form opens at once, for the next key: the connection is read again later.
    opened.value = true
    deletedNow.value = true
    confirming.value = false
  } catch {
    // Shown from the error, in the dialog.
  }
}

// Once the dialog closes, the focus goes back to its button; once the key is deleted,
// that button is gone, and the form open: to its field.
watch(confirming, async (v) => {
  if (v) return
  await nextTick()
  if (!deletedNow.value) {
    document.getElementById(deleteId)?.focus()
    return
  }
  deletedNow.value = false
  document.getElementById(field)?.focus()
  status.value = t('apiKey.deleted')
})

// Volvo's developer portal, where the user creates the application: its name is the
// same in every language.
const portal = new URL('https://developer.volvocars.com/')
</script>

<template>
  <div class="api-key-form">
    <p class="text-body-medium mb-3">
      {{ t('apiKey.intro') }}
    </p>

    <v-alert
      v-if="key?.refused_at"
      type="error"
      variant="tonal"
      :title="t('apiKey.refused.title')"
      class="refused mb-4"
    >
      {{ t('apiKey.refused.body', { time: f.dateTime(key.refused_at, now) }) }}
    </v-alert>

    <dl v-if="key" class="facts mb-4">
      <FactItem :label="t('apiKey.current')" :value="t('apiKey.last4', { last4: key.last4 })" />
      <FactItem :label="t('apiKey.setAt')" :value="f.dateTime(key.set_at, now)" />
    </dl>

    <div v-if="!open || key" class="d-flex flex-wrap ga-2 mb-4">
      <v-btn
        v-if="!open"
        :id="toggleId"
        :prepend-icon="mdiKeyVariant"
        :color="key?.refused_at ? 'primary' : undefined"
        variant="tonal"
        class="toggle"
        @click="openForm"
      >
        {{ t('apiKey.replace') }}
      </v-btn>
      <v-btn
        v-if="key"
        :id="deleteId"
        :prepend-icon="mdiDelete"
        variant="text"
        color="error"
        class="delete-key"
        @click="askDelete"
      >
        {{ t('apiKey.delete.button') }}
      </v-btn>
    </div>

    <v-dialog v-model="confirming" max-width="28rem" aria-labelledby="delete-api-key-title">
      <v-card>
        <v-card-title id="delete-api-key-title" tag="h2" class="text-title-large text-wrap">
          {{ t('apiKey.delete.title') }}
        </v-card-title>
        <v-card-text>
          <p class="text-body-medium">
            {{ t('apiKey.delete.text') }}
          </p>
          <v-alert v-if="deleteError" type="error" variant="tonal" class="mt-4">
            {{ t('apiKey.delete.error') }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="confirming = false">{{ t('apiKey.delete.cancel') }}</v-btn>
          <v-btn
            color="error"
            variant="flat"
            :loading="deletePending"
            class="confirm-delete"
            @click="confirmDelete"
          >
            {{ t('apiKey.delete.confirm') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-form v-if="open" id="api-key-form" novalidate class="key-form" @submit.prevent="submit">
      <ErrorSummary
        v-if="summary.length"
        ref="summaryRef"
        :title="t('apiKey.error.summary')"
        :items="summary"
      />
      <h3 class="text-title-medium mb-2">{{ t('apiKey.steps.title') }}</h3>
      <ol class="steps text-body-medium mb-4">
        <li>
          {{ t('apiKey.steps.signIn') }}
          <a :href="portal.href" target="_blank" rel="noreferrer" class="portal">
            {{ portal.host }}<v-icon :icon="mdiOpenInNew" size="x-small" class="ms-1" />
            <span class="d-sr-only">{{ t('apiKey.steps.newTab') }}</span>
          </a>
        </li>
        <li>{{ t('apiKey.steps.create') }}</li>
        <li>{{ t('apiKey.steps.unpublished') }}</li>
        <li>{{ t('apiKey.steps.copy') }}</li>
        <li>{{ t('apiKey.steps.paste') }}</li>
      </ol>
      <v-text-field
        :id="field"
        v-model="draft"
        :label="t('apiKey.label')"
        :error-messages="fieldError"
        :aria-invalid="fieldError ? 'true' : undefined"
        :hint="t('apiKey.help')"
        persistent-hint
        autocomplete="off"
        spellcheck="false"
        autocapitalize="none"
        class="key mb-2"
      />
      <div class="d-flex flex-wrap ga-3">
        <v-btn
          type="submit"
          color="primary"
          :prepend-icon="mdiContentSave"
          :loading="isPending"
          class="save"
        >
          {{ t('apiKey.save') }}
        </v-btn>
        <v-btn v-if="opened" variant="text" class="cancel" @click="close()">
          {{ t('apiKey.cancel') }}
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
.key {
  max-width: 32rem;
}
</style>
