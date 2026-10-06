<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { limitsPage } from '@/entities/session'
import { collectionState, needsAttention, type CollectionState } from '@/entities/vehicle'
import { useFormat, useNow } from '@/shared/lib'
import { useCollection } from '../composables/useCollection'
import { useCollectionText } from '../composables/useCollectionText'

// CollectionNotice warns, on every page of a vehicle, that nothing new is read of it,
// and why: the grant lost, with the way to connect again; no application key it may be
// read with, which the connection page takes; the collector stopped; a pause or a quota,
// which end by themselves; the account's offer leaving it unread (unread: the session's
// limits), with the page that lifts it, when the instance has one. The connection page
// tells the rest. Nothing while the vehicle is read, or before the collector's first pass.
const props = defineProps<{ vehicle: string; unread?: boolean }>()
const { t } = useI18n()
const f = useFormat()
const now = useNow()
const text = useCollectionText()
const { vehicle: v } = useCollection(() => props.vehicle)
const state = computed(() => (v.value ? collectionState(v.value, now.value, props.unread) : null))
const shown = computed(() => (state.value && needsAttention(state.value) ? state.value : null))
const connection = computed(() => v.value?.connection)

function body(s: CollectionState): string {
  switch (s.kind) {
    case 'key':
      return s.refused ? t('connection.notice.keyRefused') : t('connection.notice.keyMissing')
    case 'stopped':
      return t('connection.notice.stopped')
    case 'paused':
      return t('connection.notice.paused')
    default:
      // The quota is the key's: the account's own, or the instance's, shared.
      return connection.value?.api_key === 'own'
        ? t('connection.notice.quotaOwnKey')
        : t('connection.notice.quota')
  }
}
</script>

<template>
  <v-alert
    v-if="shown?.kind === 'unread'"
    type="info"
    variant="tonal"
    :title="text.state(shown, now)"
    class="collection-notice mb-4"
  >
    <p>{{ t('connection.notice.unread') }}</p>
    <v-btn v-if="limitsPage" :to="limitsPage" variant="outlined" color="info" class="mt-3">
      {{ t('limits.lift') }}
    </v-btn>
  </v-alert>
  <v-alert
    v-else-if="shown?.kind === 'reauth' && connection"
    type="warning"
    variant="tonal"
    :title="t('connection.notice.reauth.title')"
    class="collection-notice mb-4"
  >
    <p>{{ t('connection.notice.reauth.body') }}</p>
    <p v-if="connection.reauth_at" class="reauth-since text-body-medium mt-1">
      {{ t('connection.notice.reauth.since', { time: f.dateTime(connection.reauth_at, now) }) }}
      <template v-if="connection.reauth_reason">
        · {{ t('connection.notice.reauth.reason', { reason: connection.reauth_reason }) }}
      </template>
    </p>
    <!-- A full page load: the Volvo ID flow is served by runsten-api. -->
    <v-btn href="auth/volvo/start" color="warning" variant="flat" class="mt-3">{{
      t('connection.volvoId.reconnect')
    }}</v-btn>
  </v-alert>
  <v-alert
    v-else-if="shown"
    :type="shown.kind === 'stopped' ? 'error' : 'warning'"
    variant="tonal"
    :title="text.state(shown, now)"
    class="collection-notice mb-4"
  >
    <p>{{ body(shown) }}</p>
    <v-btn
      :to="{ name: 'connection', query: { vehicle } }"
      variant="outlined"
      :color="shown.kind === 'stopped' ? 'error' : 'warning'"
      class="mt-3"
    >
      {{ t('connection.notice.details') }}
    </v-btn>
  </v-alert>
</template>
