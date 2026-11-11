<script setup lang="ts">
import { mdiContentSave, mdiDelete, mdiKeyVariant, mdiOpenInNew } from '@mdi/js'
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ConnectionSettings, PleosBrand, PleosKey } from '@/entities/connection'
import { isApiError } from '@/shared/api'
import { useFormat, useNow } from '@/shared/lib'
import { ErrorSummary, FactItem } from '@/shared/ui'
import { useDeletePleosCredential } from '../composables/useDeletePleosCredential'
import { useSetPleosCredential } from '../composables/useSetPleosCredential'
import {
  fieldId,
  fields,
  validate,
  type Draft,
  type Field,
  type Issue,
  type Rule,
} from '../model/validate'

// PleosForm gives the account's key of one brand, which the owner creates in the Pleos
// Playground (the EU Data Act's access): a client ID and its secret. The brand is the
// section's own name; the API of each is the same, the key and the cars are not. The
// steps lead to the Playground and to the guide. The key is never shown again: the last
// four characters of its client ID, and when it was given — the API tells no expiry,
// and a key it refuses asks for another. Without one, the form is open; with one, it is
// replaced behind a button.
// off: the source is turned off for the account (the flag). A key given before stays
// told and can be removed; none is taken.
const props = defineProps<{ connection: ConnectionSettings; brand: PleosBrand; off?: boolean }>()
const { t } = useI18n()
const f = useFormat()
const now = useNow()
const { setCredential, error, reset } = useSetPleosCredential(props.brand)

const credential = computed((): PleosKey | null => props.connection.pleos?.[props.brand] ?? null)
const opened = ref(false)
const open = computed(() => !props.off && (opened.value || !credential.value))
const empty = (): Draft => ({ client_id: '', client_secret: '' })
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

const checked = computed(() => validate(draft))
const issues = computed(() => (sent.value ? checked.value.issues : []))
const labels = computed<Record<Field, string>>(() => ({
  client_id: t('pleos.field.clientId'),
  client_secret: t('pleos.field.clientSecret'),
}))
const messages: Record<Rule, (field: string, p: Record<string, number>) => string> = {
  required: (field) => t('pleos.error.required', { field }),
  spaces: (field) => t('pleos.error.spaces', { field }),
  characters: (field) => t('pleos.error.characters', { field }),
  tooShort: (field, p) => t('pleos.error.tooShort', { field, min: p.min ?? 0 }),
  tooLong: (field, p) => t('pleos.error.tooLong', { field, max: p.max ?? 0 }),
}
const message = (i: Issue) => messages[i.rule](labels.value[i.field], i.params ?? {})
const refused = computed(() => !!error.value && isApiError(error.value, 'credential_refused'))
function fieldError(field: Field): string | undefined {
  const i = issues.value.find((i) => i.field === field)
  if (i) return message(i)
  // The Playground tells the key refused, not which value: under the first.
  return refused.value && field === 'client_id' ? t('pleos.error.refused') : undefined
}
const serverError = computed(() => {
  const e = error.value
  if (!e) return null
  if (isApiError(e, 'credential_refused'))
    return { field: fieldId(props.brand, 'client_id'), text: t('pleos.error.refused') }
  if (isApiError(e, 'invalid_body'))
    return { field: fieldId(props.brand, 'client_id'), text: t('pleos.error.invalidBody') }
  if (isApiError(e, 'no_vehicle')) return { text: t('pleos.error.noVehicle') }
  if (isApiError(e, 'too_many_vehicles')) return { text: t('pleos.error.tooManyVehicles') }
  if (isApiError(e, 'not_admitted')) return { text: t('pleos.error.notAdmitted') }
  if (isApiError(e, 'source_unavailable')) return { text: t('pleos.error.sourceUnavailable') }
  if (isApiError(e, 'unavailable') || isApiError(e, 'network'))
    return { text: t('pleos.error.unavailable') }
  return { text: t('pleos.error.internal') }
})
const summary = computed(() => [
  ...issues.value.map((i) => ({ field: fieldId(props.brand, i.field), text: message(i) })),
  ...(serverError.value ? [serverError.value] : []),
])
const summaryRef = ref<InstanceType<typeof ErrorSummary> | null>(null)
async function focusSummary() {
  await nextTick()
  summaryRef.value?.focus()
}

const toggleId = computed(() => `pleos-${props.brand}-toggle`)
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
  document.getElementById(fieldId(props.brand, 'client_id'))?.focus()
}

async function close(said = '') {
  opened.value = false
  Object.assign(draft, empty())
  sent.value = false
  await nextTick()
  document.getElementById(toggleId.value)?.focus()
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
    await close(t('pleos.saved'))
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
} = useDeletePleosCredential(props.brand)
const confirming = ref(false)
const deletedNow = ref(false)
const deleteId = computed(() => `pleos-${props.brand}-delete`)

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

// Once the dialog closes, the focus goes back to its button; once the key is
// deleted, that button is gone, and the form open: to its first field.
watch(confirming, async (v) => {
  if (v) return
  await nextTick()
  if (!deletedNow.value) {
    document.getElementById(deleteId.value)?.focus()
    return
  }
  deletedNow.value = false
  document.getElementById(fieldId(props.brand, 'client_id'))?.focus()
  status.value = t('pleos.deleted')
})

// The Pleos Playground, where the owner creates the key, and Runsten's guide: their
// names are the same in every language.
const portal = new URL('https://developers.pleos.ai/')
const guide = new URL('https://runsten.app/docs/guide/hyundai-kia')
const autocomplete: Record<Field, string> = {
  client_id: 'off',
  client_secret: 'new-password',
}

// The brand, written out: an interpolated key is invisible to the unused-key rule.
// Written in full, each language keeps its own word for it.
const brandName = computed(() => {
  switch (props.brand) {
    case 'hyundai':
      return t('pleos.brand.hyundai')
    case 'kia':
      return t('pleos.brand.kia')
    default:
      return t('pleos.brand.genesis')
  }
})
</script>

<template>
  <div class="pleos-form">
    <p class="text-body-medium mb-1">
      {{ t('pleos.intro') }}
    </p>
    <p class="text-body-medium mb-3">
      <a :href="guide.href" target="_blank" rel="noreferrer" class="guide">
        {{ t('pleos.guide') }}<v-icon :icon="mdiOpenInNew" size="x-small" class="ms-1" />
        <span class="d-sr-only">{{ t('pleos.steps.newTab') }}</span>
      </a>
    </p>

    <v-alert
      v-if="credential?.refused_at"
      type="error"
      variant="tonal"
      :title="t('pleos.refused.title')"
      class="refused mb-4"
    >
      {{ t('pleos.refused.body', { time: f.dateTime(credential.refused_at, now) }) }}
    </v-alert>

    <dl v-if="credential" class="facts mb-4">
      <FactItem
        :label="t('pleos.current')"
        :value="t('pleos.last4', { last4: credential.last4 })"
      />
      <FactItem :label="t('pleos.setAt')" :value="f.dateTime(credential.set_at, now)" />
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
        {{ t('pleos.replace') }}
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
        {{ t('pleos.delete.button') }}
      </v-btn>
    </div>

    <v-dialog v-model="confirming" max-width="28rem" :aria-labelledby="`delete-${brand}-title`">
      <v-card>
        <v-card-title :id="`delete-${brand}-title`" tag="h2" class="text-title-large text-wrap">
          {{ t('pleos.delete.title', { brand: brandName }) }}
        </v-card-title>
        <v-card-text>
          <p class="text-body-medium">
            {{ t('pleos.delete.text', { brand: brandName }) }}
          </p>
          <v-alert v-if="deleteError" type="error" variant="tonal" class="mt-4">
            {{ t('pleos.delete.error') }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="confirming = false">{{ t('pleos.delete.cancel') }}</v-btn>
          <v-btn
            color="error"
            variant="flat"
            :loading="deletePending"
            class="confirm-delete"
            @click="confirmDelete"
          >
            {{ t('pleos.delete.confirm') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-form
      v-if="open"
      :id="`pleos-form-${brand}`"
      novalidate
      class="credential-form"
      @submit.prevent="submit"
    >
      <ErrorSummary
        v-if="summary.length"
        ref="summaryRef"
        :title="t('pleos.error.summary')"
        :items="summary"
      />
      <h3 class="text-title-medium mb-2">{{ t('pleos.steps.title') }}</h3>
      <ol class="steps text-body-medium mb-4">
        <li>
          {{ t('pleos.steps.signIn') }}
          <a :href="portal.href" target="_blank" rel="noreferrer" class="portal">
            {{ portal.host }}<v-icon :icon="mdiOpenInNew" size="x-small" class="ms-1" />
            <span class="d-sr-only">{{ t('pleos.steps.newTab') }}</span>
          </a>
        </li>
        <li>{{ t('pleos.steps.accept') }}</li>
        <li>{{ t('pleos.steps.get') }}</li>
        <li>{{ t('pleos.steps.copy') }}</li>
      </ol>
      <v-text-field
        v-for="field in fields"
        :id="fieldId(brand, field)"
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
      <p class="text-body-small text-medium-emphasis mb-3">{{ t('pleos.help') }}</p>
      <div class="d-flex flex-wrap ga-3">
        <v-btn
          type="submit"
          color="primary"
          :prepend-icon="mdiContentSave"
          :loading="saving"
          class="save"
        >
          {{ t('pleos.save') }}
        </v-btn>
        <v-btn v-if="opened && credential" variant="text" class="cancel" @click="close()">
          {{ t('pleos.cancel') }}
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
