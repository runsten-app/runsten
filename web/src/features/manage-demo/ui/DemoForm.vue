<script setup lang="ts">
import { mdiDelete, mdiPlayCircleOutline } from '@mdi/js'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ConnectionSettings } from '@/entities/connection'
import { isApiError } from '@/shared/api'
import { useFormat, useNow } from '@/shared/lib'
import { FactItem } from '@/shared/ui'
import { useConnectDemo } from '../composables/useConnectDemo'
import { useDisconnectDemo } from '../composables/useDisconnectDemo'

// DemoForm connects the demo vehicle, the one the instance simulates: no car, no maker's
// account, nothing to give. Connecting writes its last four weeks at once, which takes a
// few seconds; the instance then reads it as any vehicle. Disconnecting deletes it with its
// trips and charges, behind a confirmation. off: the source is turned off for the account
// (the flag): a demo vehicle connected before stays told and can be removed; none is
// connected.
const props = defineProps<{ connection: ConnectionSettings; off?: boolean }>()
const { t } = useI18n()
const f = useFormat()
const now = useNow()
const { connect, isPending, error, reset } = useConnectDemo()
const {
  disconnect,
  isPending: deletePending,
  error: deleteError,
  reset: resetDelete,
} = useDisconnectDemo()

const demo = computed(() => props.connection.demo ?? null)
const status = ref('')
const connectId = 'demo-connect'
const disconnectId = 'demo-disconnect'

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  if (isApiError(e, 'feature_unavailable')) return t('demo.error.featureUnavailable')
  if (isApiError(e, 'too_many_vehicles')) return t('demo.error.tooManyVehicles')
  if (isApiError(e, 'not_admitted')) return t('demo.error.notAdmitted')
  if (isApiError(e, 'unavailable') || isApiError(e, 'network')) return t('demo.error.unavailable')
  return t('demo.error.internal')
})

async function submit() {
  status.value = ''
  reset()
  try {
    await connect()
    status.value = t('demo.connected')
    await nextTick()
    document.getElementById(disconnectId)?.focus()
  } catch {
    // Told by the alert.
  }
}

const confirming = ref(false)
// deleted: the vehicle is deleted, the connection not yet read again without it.
const deleted = ref(false)

function askDelete() {
  status.value = ''
  resetDelete()
  confirming.value = true
}

async function confirmDelete() {
  try {
    await disconnect()
    deleted.value = true
    confirming.value = false
  } catch {
    // Shown from the error, in the dialog.
  }
}

// Once the dialog closes, the focus goes back to its button, unless the vehicle is
// deleted: that button goes with it, and the focus to the one that connects it again,
// once the connection says so.
watch(confirming, async (v) => {
  if (v || deleted.value) return
  await nextTick()
  document.getElementById(disconnectId)?.focus()
})
watch([demo, deleted], async ([d, done]) => {
  if (d || !done) return
  deleted.value = false
  status.value = t('demo.disconnected')
  await nextTick()
  document.getElementById(connectId)?.focus()
})
</script>

<template>
  <div class="demo-form">
    <p class="text-body-medium mb-4">{{ t('demo.intro') }}</p>

    <dl v-if="demo" class="facts mb-4">
      <FactItem :label="t('demo.status')" :value="t('demo.active')" />
      <FactItem :label="t('demo.setAt')" :value="f.dateTime(demo.set_at, now)" />
    </dl>

    <template v-if="!demo && !off">
      <v-alert v-if="errorText" type="error" variant="tonal" class="error mb-4">
        {{ errorText }}
      </v-alert>
      <div class="d-flex flex-wrap align-center ga-3">
        <v-btn
          :id="connectId"
          color="primary"
          :prepend-icon="mdiPlayCircleOutline"
          :loading="isPending"
          class="connect"
          @click="submit"
        >
          {{ t('demo.connect') }}
        </v-btn>
        <span class="text-body-small text-medium-emphasis">{{ t('demo.help') }}</span>
      </div>
    </template>

    <v-btn
      v-if="demo"
      :id="disconnectId"
      :prepend-icon="mdiDelete"
      variant="text"
      color="error"
      class="disconnect"
      @click="askDelete"
    >
      {{ t('demo.disconnect.button') }}
    </v-btn>

    <v-dialog v-model="confirming" max-width="28rem" aria-labelledby="delete-demo-title">
      <v-card>
        <v-card-title id="delete-demo-title" tag="h2" class="text-title-large text-wrap">
          {{ t('demo.disconnect.title') }}
        </v-card-title>
        <v-card-text>
          <p class="text-body-medium">{{ t('demo.disconnect.text') }}</p>
          <v-alert v-if="deleteError" type="error" variant="tonal" class="mt-4">
            {{ t('demo.disconnect.error') }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="confirming = false">
            {{ t('demo.disconnect.cancel') }}
          </v-btn>
          <v-btn
            color="error"
            variant="flat"
            :loading="deletePending"
            class="confirm-disconnect"
            @click="confirmDelete"
          >
            {{ t('demo.disconnect.confirm') }}
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
</style>
