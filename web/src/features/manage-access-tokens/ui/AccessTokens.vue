<script setup lang="ts">
import { mdiCheck, mdiContentCopy, mdiDelete, mdiKeyPlus } from '@mdi/js'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AccessToken, CreatedAccessToken, Expiry } from '@/entities/access-token'
import { isApiError } from '@/shared/api'
import { useFormat, useNow } from '@/shared/lib'
import { ErrorSummary } from '@/shared/ui'
import {
  useAccessTokens,
  useCreateAccessToken,
  useRevokeAccessToken,
} from '../composables/useAccessTokens'

// AccessTokens lists the user's personal access tokens, issues one, and revokes them. A
// token lets a program (a script, Home Assistant, an assistant) read the API without
// the user's password; it only reads. Its secret is shown once, right after it is
// issued, with a button to copy it: only its hash is kept.
const { t } = useI18n()
const f = useFormat()
const now = useNow()
const { tokens, isPending, error } = useAccessTokens()

// maxName is the API's bound of a token's name.
const maxName = 64
const nameId = 'access-token-name'
const toggleId = 'access-token-new'
const opened = ref(false)
const name = ref('')
const expiry = ref<Expiry>('90d')
const sent = ref(false)
const status = ref('')
const created = ref<CreatedAccessToken | null>(null)
const copied = ref(false)

// The lifetimes a token may be given, named one by one: the i18n lint sees every key.
const expiries = computed((): { value: Expiry; title: string }[] => [
  { value: '30d', title: t('accessTokens.expiry.30d') },
  { value: '90d', title: t('accessTokens.expiry.90d') },
  { value: '365d', title: t('accessTokens.expiry.365d') },
  { value: 'never', title: t('accessTokens.expiry.never') },
])

const {
  createAccessToken,
  isPending: creating,
  error: createError,
  reset: resetCreate,
} = useCreateAccessToken()
watch(name, () => resetCreate())

const nameIssue = computed(() => {
  if (!sent.value) return undefined
  const n = name.value.trim()
  if (!n) return t('accessTokens.error.required')
  if ([...n].length > maxName) return t('accessTokens.error.tooLong', { max: maxName })
  return undefined
})
const summary = computed(() => {
  const items: { field?: string; text: string }[] = []
  if (nameIssue.value) items.push({ field: nameId, text: nameIssue.value })
  const e = createError.value
  if (e && isApiError(e, 'invalid_body'))
    items.push({ field: nameId, text: t('accessTokens.error.invalid') })
  else if (e && (isApiError(e, 'unavailable') || isApiError(e, 'network')))
    items.push({ text: t('accessTokens.error.unavailable') })
  else if (e) items.push({ text: t('accessTokens.error.internal') })
  return items
})
const summaryRef = ref<InstanceType<typeof ErrorSummary> | null>(null)

async function openForm() {
  name.value = ''
  expiry.value = '90d'
  sent.value = false
  created.value = null
  status.value = ''
  opened.value = true
  await nextTick()
  resetCreate()
  document.getElementById(nameId)?.focus()
}

async function closeForm() {
  opened.value = false
  await nextTick()
  document.getElementById(toggleId)?.focus()
}

async function submit() {
  sent.value = true
  if (nameIssue.value) {
    await nextTick()
    return summaryRef.value?.focus()
  }
  try {
    created.value = await createAccessToken({ name: name.value.trim(), expiry: expiry.value })
    opened.value = false
    copied.value = false
    await nextTick()
    document.getElementById('access-token-secret')?.focus()
  } catch {
    await nextTick()
    summaryRef.value?.focus()
  }
}

async function copy() {
  if (!created.value) return
  try {
    await navigator.clipboard.writeText(created.value.token)
    copied.value = true
    status.value = t('accessTokens.created.copied')
  } catch {
    // Without the clipboard (a page over plain http), the field stays to copy by hand.
    status.value = t('accessTokens.created.copyFailed')
  }
}

// header is the HTTP header a program sends, the same in every language.
const header = computed(() => `Authorization: Bearer ${created.value?.token ?? ''}`)

async function done() {
  created.value = null
  await nextTick()
  document.getElementById(toggleId)?.focus()
}

const usage = (iso: string | null) =>
  iso ? f.dateTime(iso, now.value) : t('accessTokens.neverUsed')
const expired = (tok: AccessToken) =>
  tok.expires_at !== null && Date.parse(tok.expires_at) <= now.value
const ends = (tok: AccessToken) => {
  if (tok.expires_at === null) return t('accessTokens.noExpiry')
  const at = f.dateTime(tok.expires_at, now.value)
  return expired(tok) ? t('accessTokens.expiredAt', { at }) : t('accessTokens.expiresAt', { at })
}

const {
  revokeAccessToken,
  isPending: revoking,
  error: revokeError,
  reset: resetRevoke,
} = useRevokeAccessToken()
const revoked = ref<AccessToken | null>(null)
const confirming = computed({
  get: () => revoked.value !== null,
  set: (v) => {
    if (!v) revoked.value = null
  },
})

function askRevoke(tok: AccessToken) {
  status.value = ''
  resetRevoke()
  revoked.value = tok
}

async function confirmRevoke() {
  const tok = revoked.value
  if (!tok) return
  try {
    await revokeAccessToken(tok.id)
    revoked.value = null
    await nextTick()
    document.getElementById(toggleId)?.focus()
    status.value = t('accessTokens.revoke.done', { name: tok.name })
  } catch {
    // Shown from the error, in the dialog.
  }
}
</script>

<template>
  <div class="access-tokens">
    <p class="text-body-medium mb-4">{{ t('accessTokens.help') }}</p>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">
      {{ t('accessTokens.error.list') }}
    </v-alert>
    <v-progress-linear
      v-else-if="isPending"
      indeterminate
      :aria-label="t('accessTokens.loading')"
      class="mb-4"
    />
    <p v-else-if="!tokens?.length" class="none text-body-medium mb-4">
      {{ t('accessTokens.none') }}
    </p>
    <v-list v-else class="tile-list tokens py-0 mb-4">
      <div v-for="(tok, i) in tokens" :key="tok.id" role="listitem" class="token">
        <v-divider v-if="i > 0" />
        <div class="d-flex flex-wrap align-center ga-2 px-4 py-3">
          <div class="flex-grow-1">
            <p class="text-title-medium text-wrap name">{{ tok.name }}</p>
            <p class="text-body-medium text-medium-emphasis">
              {{ t('accessTokens.createdAt', { at: f.dateTime(tok.created_at, now) }) }} ·
              {{ t('accessTokens.lastUsed', { at: usage(tok.last_used_at) }) }} ·
              <span :class="{ 'text-error': expired(tok) }" class="ends">{{ ends(tok) }}</span>
            </p>
          </div>
          <v-btn
            :prepend-icon="mdiDelete"
            variant="text"
            color="error"
            class="revoke"
            :aria-label="t('accessTokens.revoke.label', { name: tok.name })"
            @click="askRevoke(tok)"
          >
            {{ t('accessTokens.revoke.button') }}
          </v-btn>
        </div>
      </div>
    </v-list>

    <v-alert
      v-if="created"
      type="success"
      variant="tonal"
      :title="t('accessTokens.created.title', { name: created.name })"
      class="created mb-4"
    >
      <p class="text-body-medium mb-3">{{ t('accessTokens.created.once') }}</p>
      <div class="d-flex flex-wrap align-center ga-2">
        <v-text-field
          id="access-token-secret"
          :model-value="created.token"
          :label="t('accessTokens.created.label')"
          readonly
          hide-details
          class="secret"
          @focus="($event.target as HTMLInputElement).select()"
        />
        <v-btn
          :prepend-icon="copied ? mdiCheck : mdiContentCopy"
          variant="tonal"
          class="copy"
          @click="copy"
        >
          {{ t('accessTokens.created.copy') }}
        </v-btn>
      </div>
      <p class="text-body-medium mt-3">{{ t('accessTokens.created.use') }}</p>
      <pre class="example text-body-small mt-1"><code>{{ header }}</code></pre>
      <v-btn variant="text" class="done mt-2" @click="done">
        {{ t('accessTokens.created.done') }}
      </v-btn>
    </v-alert>

    <v-btn
      v-if="!opened && !created"
      :id="toggleId"
      :prepend-icon="mdiKeyPlus"
      variant="tonal"
      class="new"
      @click="openForm"
    >
      {{ t('accessTokens.new') }}
    </v-btn>

    <v-form v-if="opened" novalidate class="token-form" @submit.prevent="submit">
      <ErrorSummary
        v-if="summary.length && sent"
        ref="summaryRef"
        :title="t('accessTokens.error.summary')"
        :items="summary"
      />
      <v-text-field
        :id="nameId"
        v-model="name"
        :label="t('accessTokens.name')"
        :hint="t('accessTokens.nameHelp')"
        persistent-hint
        :error-messages="nameIssue"
        :aria-invalid="nameIssue ? 'true' : undefined"
        :counter="maxName"
        autocomplete="off"
        class="name-field mb-2"
      />
      <v-select
        id="access-token-expiry"
        v-model="expiry"
        :items="expiries"
        :label="t('accessTokens.expiry.label')"
        class="expiry mb-2"
      />
      <div class="d-flex flex-wrap ga-3">
        <v-btn type="submit" color="primary" :loading="creating" class="create">
          {{ t('accessTokens.create') }}
        </v-btn>
        <v-btn variant="text" class="cancel" @click="closeForm">
          {{ t('accessTokens.cancel') }}
        </v-btn>
      </div>
    </v-form>

    <v-dialog v-model="confirming" max-width="28rem" aria-labelledby="revoke-token-title">
      <v-card v-if="revoked">
        <v-card-title id="revoke-token-title" tag="h2" class="text-title-large text-wrap">
          {{ t('accessTokens.revoke.title', { name: revoked.name }) }}
        </v-card-title>
        <v-card-text>
          <p class="text-body-medium">{{ t('accessTokens.revoke.text') }}</p>
          <v-alert v-if="revokeError" type="error" variant="tonal" class="mt-4">
            {{ t('accessTokens.revoke.error') }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="confirming = false">
            {{ t('accessTokens.cancel') }}
          </v-btn>
          <v-btn
            color="error"
            variant="flat"
            :loading="revoking"
            class="confirm-revoke"
            @click="confirmRevoke"
          >
            {{ t('accessTokens.revoke.confirm') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
    <p role="status" class="status text-body-medium mt-2">{{ status }}</p>
  </div>
</template>

<style scoped>
.name-field,
.expiry,
.secret {
  max-width: 32rem;
}
.secret {
  min-width: min(100%, 20rem);
}
.example {
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
