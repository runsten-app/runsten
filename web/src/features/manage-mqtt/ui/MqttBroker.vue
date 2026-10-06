<script setup lang="ts">
import { mdiContentSave, mdiDelete, mdiPencil } from '@mdi/js'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MqttFailure, MqttStatus } from '@/entities/mqtt'
import { isApiError } from '@/shared/api'
import { useFormat, useNow } from '@/shared/lib'
import { ErrorSummary, FactItem } from '@/shared/ui'
import { useDeleteMqttBroker, useMqtt, useSetMqttBroker } from '../composables/useMqtt'
import { draftOf, field, validate, type Draft, type Issue, type Rule } from '../model/validate'

// MqttBroker sets the account's MQTT broker, which the collector publishes the vehicles'
// state to, for Home Assistant and the like, and tells how the collector's connection to
// it goes. Its password is never shown again: whether there is one only. Without a
// broker, the form is open; with one, it is changed behind a button. locked: the
// account's offer leaves MQTT out: a broker set before is shown, and may be removed.
const props = defineProps<{ locked: boolean }>()
const { t } = useI18n()
const f = useFormat()
const now = useNow()
const { mqtt, isPending: loading, error: loadError } = useMqtt()
const { setMqttBroker, isPending, error, reset } = useSetMqttBroker()

const broker = computed(() => mqtt.value?.broker ?? null)
const publicOnly = computed(() => mqtt.value?.public_only ?? false)
const opened = ref(false)
const open = computed(() => !props.locked && (opened.value || (!!mqtt.value && !broker.value)))
const draft = ref<Draft>(draftOf(null))
const sent = ref(false)
const status = ref('')

watch(
  draft,
  () => {
    status.value = ''
    reset()
  },
  { deep: true },
)

const checked = computed(() => validate(draft.value, broker.value, publicOnly.value))
const issues = computed(() => (sent.value ? checked.value.issues : []))
const messages: Record<Rule, (p: Record<string, number>) => string> = {
  required: () => t('mqtt.error.required'),
  scheme: () => t('mqtt.error.scheme'),
  tls: () => t('mqtt.error.tls'),
  host: () => t('mqtt.error.host'),
  credentials: () => t('mqtt.error.credentials'),
  path: () => t('mqtt.error.path'),
  port: () => t('mqtt.error.port'),
  tooLong: (p) => t('mqtt.error.tooLong', { max: p.max ?? 0 }),
  passwordAgain: () => t('mqtt.error.passwordAgain'),
  prefix: () => t('mqtt.error.prefix'),
  clientId: () => t('mqtt.error.clientId'),
}
const labels = computed<Record<string, string>>(() => ({
  [field.url]: t('mqtt.url'),
  [field.username]: t('mqtt.username'),
  [field.password]: t('mqtt.password'),
  [field.clientId]: t('mqtt.clientId'),
  [field.topicPrefix]: t('mqtt.topicPrefix'),
  [field.discoveryPrefix]: t('mqtt.discoveryPrefix'),
}))
const message = (i: Issue) => messages[i.rule](i.params ?? {})
const serverError = computed(() => {
  const e = error.value
  if (!e) return null
  if (isApiError(e, 'broker_refused')) return { field: field.url, text: t('mqtt.error.refused') }
  if (isApiError(e, 'invalid_body')) return { text: t('mqtt.error.invalidBody') }
  if (isApiError(e, 'feature_unavailable')) return { text: t('mqtt.error.featureUnavailable') }
  if (isApiError(e, 'unavailable') || isApiError(e, 'network'))
    return { text: t('mqtt.error.unavailable') }
  return { text: t('mqtt.error.internal') }
})
const errorOf = (id: string) => {
  const i = issues.value.find((i) => i.field === id)
  if (i) return message(i)
  return serverError.value && 'field' in serverError.value && serverError.value.field === id
    ? serverError.value.text
    : undefined
}
const summary = computed(() => [
  ...issues.value.map((i) => ({
    field: i.field,
    text: t('mqtt.error.item', { field: labels.value[i.field] ?? '', message: message(i) }),
  })),
  ...(serverError.value ? [serverError.value] : []),
])
const summaryRef = ref<InstanceType<typeof ErrorSummary> | null>(null)
async function focusSummary() {
  await nextTick()
  summaryRef.value?.focus()
}

// The advanced fields are folded until one of them has an issue.
const advanced = ref(false)
watch(issues, (list) => {
  if (list.some((i) => i.field === field.clientId || i.field.endsWith('prefix'))) {
    advanced.value = true
  }
})

const toggleId = 'mqtt-edit'
// Opened on request, the form focuses its first field once shown: no autofocus
// (Vuetify's takes the focus back 50 ms later).
async function openForm() {
  draft.value = draftOf(broker.value)
  sent.value = false
  advanced.value = false
  opened.value = true
  await nextTick()
  reset()
  status.value = ''
  document.getElementById(field.url)?.focus()
}

async function close(said = '') {
  opened.value = false
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
    await setMqttBroker(body)
    await close(t('mqtt.saved'))
  } catch {
    await focusSummary()
  }
}

// The collector's connection: down when its latest failure is after its latest
// connection; waiting when it has written nothing yet of this configuration.
const failure = computed(() => {
  const s: MqttStatus | null = mqtt.value?.status ?? null
  if (!s?.failed_at || !s.failure) return null
  if (s.connected_at && Date.parse(s.connected_at) > Date.parse(s.failed_at)) return null
  return { kind: s.failure, at: s.failed_at }
})
const connectedAt = computed(() => mqtt.value?.status?.connected_at ?? null)
const failures: Record<MqttFailure, () => string> = {
  unreachable: () => t('mqtt.failure.unreachable'),
  refused_address: () => t('mqtt.failure.refusedAddress'),
  tls: () => t('mqtt.failure.tls'),
  not_authorized: () => t('mqtt.failure.notAuthorized'),
  rejected: () => t('mqtt.failure.rejected'),
}

const {
  deleteMqttBroker,
  isPending: deletePending,
  error: deleteError,
  reset: resetDelete,
} = useDeleteMqttBroker()
const confirming = ref(false)
const deletedNow = ref(false)
const deleteId = 'mqtt-delete'

function askDelete() {
  status.value = ''
  resetDelete()
  confirming.value = true
}

async function confirmDelete() {
  try {
    await deleteMqttBroker()
    // The form opens at once for another broker: the broker is read again later.
    draft.value = draftOf(null)
    sent.value = false
    opened.value = !props.locked
    deletedNow.value = true
    confirming.value = false
  } catch {
    // Shown from the error, in the dialog.
  }
}

// Once the dialog closes, the focus goes back to its button; once the broker is
// removed, that button is gone: to the form's first field, if open.
watch(confirming, async (v) => {
  if (v) return
  await nextTick()
  if (!deletedNow.value) {
    document.getElementById(deleteId)?.focus()
    return
  }
  deletedNow.value = false
  document.getElementById(field.url)?.focus()
  status.value = t('mqtt.deleted')
})
</script>

<template>
  <div class="mqtt-broker">
    <p class="text-body-medium mb-3">{{ t('mqtt.help') }}</p>
    <v-alert v-if="loadError" type="error" variant="tonal">{{ t('mqtt.loadError') }}</v-alert>
    <v-progress-linear v-else-if="loading" indeterminate :aria-label="t('mqtt.loading')" />
    <template v-else>
      <template v-if="broker">
        <v-alert
          v-if="failure"
          type="warning"
          variant="tonal"
          :title="t('mqtt.failure.title')"
          class="failure mb-4"
        >
          {{ failures[failure.kind]() }}
          {{ t('mqtt.failure.at', { time: f.dateTime(failure.at, now) }) }}
        </v-alert>
        <p v-else-if="connectedAt" class="connected text-body-medium mb-4">
          {{ t('mqtt.connected', { time: f.dateTime(connectedAt, now) }) }}
        </p>
        <p v-else class="waiting text-body-medium mb-4">{{ t('mqtt.waiting') }}</p>

        <dl class="facts mb-4">
          <FactItem class="url" :label="t('mqtt.url')" :value="broker.url" />
          <FactItem :label="t('mqtt.username')" :value="broker.username || t('mqtt.none')" />
          <FactItem
            :label="t('mqtt.password')"
            :value="broker.password_set ? t('mqtt.passwordSet') : t('mqtt.none')"
          />
          <FactItem :label="t('mqtt.topicPrefix')" :value="broker.topic_prefix" />
          <FactItem
            :label="t('mqtt.discovery')"
            :value="
              broker.discovery
                ? t('mqtt.discoveryOn', { prefix: broker.discovery_prefix })
                : t('mqtt.off')
            "
          />
          <FactItem
            :label="t('mqtt.publishLocation')"
            :value="broker.publish_location ? t('mqtt.on') : t('mqtt.off')"
          />
        </dl>

        <div v-if="!open" class="d-flex flex-wrap ga-2 mb-4">
          <v-btn
            v-if="!locked"
            :id="toggleId"
            :prepend-icon="mdiPencil"
            variant="tonal"
            class="toggle"
            @click="openForm"
          >
            {{ t('mqtt.edit') }}
          </v-btn>
          <v-btn
            :id="deleteId"
            :prepend-icon="mdiDelete"
            variant="text"
            color="error"
            class="delete-broker"
            @click="askDelete"
          >
            {{ t('mqtt.delete.button') }}
          </v-btn>
        </div>
      </template>

      <v-form v-if="open" id="mqtt-form" novalidate class="broker-form" @submit.prevent="submit">
        <ErrorSummary
          v-if="summary.length"
          ref="summaryRef"
          :title="t('mqtt.error.summary')"
          :items="summary"
        />
        <v-text-field
          :id="field.url"
          v-model="draft.url"
          :label="t('mqtt.url')"
          :error-messages="errorOf(field.url)"
          :aria-invalid="errorOf(field.url) ? 'true' : undefined"
          :hint="publicOnly ? t('mqtt.urlHelpPublic') : t('mqtt.urlHelp')"
          persistent-hint
          inputmode="url"
          autocomplete="off"
          spellcheck="false"
          autocapitalize="none"
          class="wide mb-2"
        />
        <div class="d-flex flex-wrap ga-3 mt-2">
          <v-text-field
            :id="field.username"
            v-model="draft.username"
            :label="t('mqtt.username')"
            :error-messages="errorOf(field.username)"
            :aria-invalid="errorOf(field.username) ? 'true' : undefined"
            :hint="t('mqtt.usernameHelp')"
            persistent-hint
            autocomplete="off"
            spellcheck="false"
            autocapitalize="none"
            class="half"
          />
          <v-text-field
            :id="field.password"
            v-model="draft.password"
            type="password"
            :label="t('mqtt.password')"
            :error-messages="errorOf(field.password)"
            :aria-invalid="errorOf(field.password) ? 'true' : undefined"
            :hint="broker?.password_set ? t('mqtt.passwordKept') : t('mqtt.passwordHelp')"
            :disabled="draft.removePassword"
            persistent-hint
            autocomplete="new-password"
            class="half"
          />
        </div>
        <v-checkbox
          v-if="broker?.password_set"
          v-model="draft.removePassword"
          :label="t('mqtt.removePassword')"
          hide-details
          class="remove-password mt-2"
        />
        <v-checkbox
          v-model="draft.discovery"
          :label="t('mqtt.discovery')"
          :hint="t('mqtt.discoveryHelp')"
          persistent-hint
          class="mt-2"
        />
        <v-checkbox
          v-model="draft.publishLocation"
          :label="t('mqtt.publishLocation')"
          :hint="t('mqtt.publishLocationHelp')"
          persistent-hint
          class="mt-2"
        />

        <details
          :open="advanced"
          class="advanced mt-4"
          @toggle="advanced = ($event.target as HTMLDetailsElement).open"
        >
          <summary class="text-title-small">{{ t('mqtt.advanced') }}</summary>
          <div class="d-flex flex-wrap ga-3 mt-3">
            <v-text-field
              :id="field.topicPrefix"
              v-model="draft.topicPrefix"
              :label="t('mqtt.topicPrefix')"
              :error-messages="errorOf(field.topicPrefix)"
              :aria-invalid="errorOf(field.topicPrefix) ? 'true' : undefined"
              :hint="t('mqtt.topicPrefixHelp')"
              persistent-hint
              autocomplete="off"
              spellcheck="false"
              autocapitalize="none"
              class="half"
            />
            <v-text-field
              :id="field.discoveryPrefix"
              v-model="draft.discoveryPrefix"
              :label="t('mqtt.discoveryPrefix')"
              :error-messages="errorOf(field.discoveryPrefix)"
              :aria-invalid="errorOf(field.discoveryPrefix) ? 'true' : undefined"
              :hint="t('mqtt.discoveryPrefixHelp')"
              persistent-hint
              autocomplete="off"
              spellcheck="false"
              autocapitalize="none"
              class="half"
            />
            <v-text-field
              :id="field.clientId"
              v-model="draft.clientId"
              :label="t('mqtt.clientId')"
              :error-messages="errorOf(field.clientId)"
              :aria-invalid="errorOf(field.clientId) ? 'true' : undefined"
              :hint="t('mqtt.clientIdHelp')"
              persistent-hint
              autocomplete="off"
              spellcheck="false"
              autocapitalize="none"
              class="half"
            />
          </div>
        </details>

        <div class="d-flex flex-wrap ga-3 mt-4">
          <v-btn
            type="submit"
            color="primary"
            :prepend-icon="mdiContentSave"
            :loading="isPending"
            class="save"
          >
            {{ t('mqtt.save') }}
          </v-btn>
          <v-btn v-if="broker" variant="text" class="cancel" @click="close()">
            {{ t('mqtt.cancel') }}
          </v-btn>
        </div>
      </v-form>
    </template>

    <v-dialog v-model="confirming" max-width="28rem" aria-labelledby="delete-mqtt-title">
      <v-card>
        <v-card-title id="delete-mqtt-title" tag="h2" class="text-title-large text-wrap">
          {{ t('mqtt.delete.title') }}
        </v-card-title>
        <v-card-text>
          <p class="text-body-medium">{{ t('mqtt.delete.text') }}</p>
          <v-alert v-if="deleteError" type="error" variant="tonal" class="mt-4">
            {{ t('mqtt.delete.error') }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="confirming = false">{{ t('mqtt.delete.cancel') }}</v-btn>
          <v-btn
            color="error"
            variant="flat"
            :loading="deletePending"
            class="confirm-delete"
            @click="confirmDelete"
          >
            {{ t('mqtt.delete.confirm') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
    <p role="status" class="status text-body-medium mt-2">{{ status }}</p>
  </div>
</template>

<style scoped>
.facts {
  display: grid;
  gap: 1rem;
  grid-template-columns: repeat(auto-fill, minmax(14rem, 1fr));
}
/* A URL has no space to wrap at: its own row, cut anywhere. */
.facts .url {
  grid-column: 1 / -1;
  overflow-wrap: anywhere;
}
.wide {
  max-width: 32rem;
}
.half {
  flex: 1 1 14rem;
  max-width: 20rem;
}
.advanced summary {
  cursor: pointer;
  width: fit-content;
}
</style>
